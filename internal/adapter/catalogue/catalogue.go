// Package catalogue implements the Catalogue port for the published
// catalogue, from the catalogue service: one call per project, a recipe's
// files by their Git blob hash, each checked as it arrives and the folder
// against its revision, and every answer checked against spec/catalogue/v0.
// A recipe folder is kept by revision and never fetched again; a project's
// last answer is replaced in place, trusted for a minute, and used when the
// service cannot be reached. The user's GitHub login is sent only when the
// service asks for it.
package catalogue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/admission"
	cat "github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/recipe"
	"github.com/auscaster/ugt/spec"
)

// Client reads the catalogue service.
type Client struct {
	base  string // the API's root, ending in /
	http  *http.Client
	token func() string // the user's GitHub login, or ""
	cache string        // a folder of its own
	ttl   time.Duration
	now   func() time.Time
}

var _ app.Catalogue = (*Client)(nil)

// New returns a client for the catalogue service at base, keeping what it
// fetches in cache.
func New(base string, token func() string, cache string) *Client {
	return &Client{base: strings.TrimSuffix(base, "/") + "/", http: &http.Client{Timeout: 30 * time.Second},
		token: token, cache: cache, ttl: time.Minute, now: time.Now}
}

func (c *Client) Source() cat.Source { return cat.Published }

// maxFile bounds one recipe file: recipes hold configuration, scripts and
// small samples.
const maxFile = 16 << 20

// Project lists the admitted recipes of owner/repo.
func (c *Client) Project(ctx context.Context, owner, repo string) ([]cat.Entry, error) {
	ans, err := c.project(ctx, strings.ToLower(owner+"/"+repo))
	if err != nil {
		return nil, err
	}
	return entries(ans.Recipes), nil
}

// Search lists the admitted recipes that match any of the words, best first.
func (c *Client) Search(ctx context.Context, words []string) ([]cat.Entry, error) {
	if len(words) == 0 {
		return nil, nil
	}
	data, _, status, err := c.get(ctx, "search?q="+url.QueryEscape(strings.Join(words, " ")), "")
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, refused(status, data)
	}
	var ans struct {
		Recipes []answer `json:"recipes"`
	}
	if err := decode(data, "#/$defs/search", &ans); err != nil {
		return nil, err
	}
	return entries(ans.Recipes), nil
}

// Open returns a recipe's folder at its current revision.
func (c *Client) Open(ctx context.Context, id string) (fs.FS, error) {
	parsed, err := recipe.ParseID(id)
	if err != nil {
		return nil, err
	}
	ans, err := c.project(ctx, parsed.Owner+"/"+parsed.Repo)
	if err != nil {
		return nil, err
	}
	for _, r := range ans.Recipes {
		if r.Recipe == id {
			dir, err := c.folder(ctx, r)
			if err != nil {
				return nil, err
			}
			return os.DirFS(dir), nil
		}
	}
	return nil, fmt.Errorf("%s in the catalogue: %w", id, app.ErrNotFound)
}

// answer is one recipe as the service describes it.
type answer struct {
	Recipe   string               `json:"recipe"`
	Revision string               `json:"revision"`
	Run      string               `json:"run"`
	Task     string               `json:"task"`
	Kind     string               `json:"kind"`
	Repo     string               `json:"repo"`
	Commit   string               `json:"commit"`
	Licence  string               `json:"licence"`
	Settings []recipe.Setting     `json:"settings"`
	Access   []recipe.Access      `json:"access"`
	Inputs   []recipe.File        `json:"inputs"`
	Outputs  []recipe.File        `json:"outputs"`
	Provides recipe.Provides      `json:"provides"`
	Files    []file               `json:"files"`
	Images   map[string]cat.Image `json:"images"`
	Verified []cat.Verification   `json:"verified"`
}

type file struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}

type projectAnswer struct {
	Project string   `json:"project"`
	Recipes []answer `json:"recipes"`
}

func entries(answers []answer) []cat.Entry {
	out := make([]cat.Entry, len(answers))
	for i, r := range answers {
		out[i] = cat.Entry{
			Recipe: r.Recipe, What: r.Task, Kind: r.Kind, Repo: r.Repo, Revision: r.Revision, Run: r.Run, Source: cat.Published,
			Inputs: r.Inputs, Outputs: r.Outputs, Provides: r.Provides, Settings: r.Settings, Access: r.Access, Images: r.Images, Verified: r.Verified,
		}
	}
	return out
}

// kept is a project's last answer, as the cache holds it.
type kept struct {
	ETag    string          `json:"etag"`
	Checked time.Time       `json:"checked"`
	Body    json.RawMessage `json:"body"`
}

var keepMu sync.Mutex

// project returns a project's answer: from the cache within its minute, with
// a conditional request after it, and from the cache when the service cannot
// be reached or fails.
func (c *Client) project(ctx context.Context, key string) (projectAnswer, error) {
	keepMu.Lock()
	defer keepMu.Unlock()
	file := filepath.Join(c.cache, "projects", filepath.FromSlash(key)+".json")
	var last kept
	if data, err := os.ReadFile(file); err == nil && json.Unmarshal(data, &last) == nil && len(last.Body) > 0 {
		if c.now().Sub(last.Checked) < c.ttl {
			return parseProject(last.Body)
		}
	} else {
		last = kept{}
	}
	data, etag, status, err := c.get(ctx, "projects/"+key, last.ETag)
	switch {
	case err != nil && last.Body != nil:
		return parseProject(last.Body) // offline: the last answer
	case err != nil:
		return projectAnswer{}, err
	case status == http.StatusNotModified && last.Body != nil:
		data = last.Body
	case status == http.StatusOK:
		if _, err := parseProject(data); err != nil {
			return projectAnswer{}, err
		}
		last.ETag = etag
	case status >= 500 && last.Body != nil:
		return parseProject(last.Body)
	default:
		return projectAnswer{}, refused(status, data)
	}
	last.Body, last.Checked = data, c.now()
	if err := writeKept(file, last); err != nil {
		return projectAnswer{}, err
	}
	return parseProject(data)
}

func parseProject(data []byte) (projectAnswer, error) {
	var ans projectAnswer
	err := decode(data, "#/$defs/project", &ans)
	return ans, err
}

func writeKept(file string, k kept) error {
	data, err := json.Marshal(k)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// folder makes sure a recipe's folder is in the cache, by revision, and
// returns where.
func (c *Client) folder(ctx context.Context, r answer) (string, error) {
	dest := filepath.Join(c.cache, "recipes", r.Revision)
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), r.Revision+".*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	for _, f := range r.Files {
		if clean := path.Clean(f.Path); clean != f.Path || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
			return "", fmt.Errorf("%s: the catalogue named a file outside the recipe, %q", r.Recipe, f.Path)
		}
		data, _, status, err := c.get(ctx, "blobs/"+f.SHA, "")
		if err != nil {
			return "", err
		}
		if status != http.StatusOK {
			return "", refused(status, data)
		}
		if got := admission.BlobSHA(data); got != f.SHA {
			return "", fmt.Errorf("%s/%s arrived with Git hash %s, not %s: not using it", r.Recipe, f.Path, got, f.SHA)
		}
		mode := os.FileMode(0o644)
		if f.Mode == "100755" {
			mode = 0o755
		}
		target := filepath.Join(tmp, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return "", err
		}
	}
	if rev, err := recipe.Revision(os.DirFS(tmp)); err != nil || rev != r.Revision {
		return "", fmt.Errorf("%s arrived as revision %s, not %s: not using it (%v)", r.Recipe, rev, r.Revision, err)
	}
	// complete or not at all: another process may have finished it first
	if err := os.Rename(tmp, dest); err != nil {
		if _, serr := os.Stat(dest); serr != nil {
			return "", err
		}
	}
	return dest, nil
}

// get fetches a path of the API.
func (c *Client) get(ctx context.Context, p, etag string) (body []byte, gotETag string, status int, err error) {
	resp, err := c.do(ctx, http.MethodGet, p, nil, etag)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxFile+1))
	if err != nil {
		return nil, "", 0, err
	}
	if len(body) > maxFile {
		return nil, "", 0, fmt.Errorf("the catalogue answered more than %d MB for %s", maxFile>>20, p)
	}
	return body, resp.Header.Get("ETag"), resp.StatusCode, nil
}

// Report tells the service how a run of one of its recipes went. A report
// outside the contract, which holds identifiers only, is never sent.
func (c *Client) Report(ctx context.Context, r cat.Run) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := conforms(data, "#/$defs/run"); err != nil {
		return fmt.Errorf("a run report outside the catalogue's contract is not sent: %w", err)
	}
	resp, err := c.do(ctx, http.MethodPost, "runs", data, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return refused(resp.StatusCode, body)
	}
	return nil
}

// do sends one request to the API. A request the service challenges for a
// GitHub login is sent again with the user's, when they have one.
func (c *Client) do(ctx context.Context, method, p string, body []byte, etag string) (*http.Response, error) {
	send := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, c.base+p, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "ugt")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return c.http.Do(req)
	}
	resp, err := send("")
	if err == nil && resp.StatusCode == http.StatusUnauthorized && c.token != nil {
		if token := c.token(); token != "" {
			resp.Body.Close()
			resp, err = send(token)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("could not reach the catalogue at %s: %w", c.base, err)
	}
	return resp, nil
}

// refused turns a status the service answered into the error it explains.
func refused(status int, body []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	msg := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		msg = e.Error
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	if status == http.StatusNotFound {
		return fmt.Errorf("the catalogue: %w (%s)", app.ErrNotFound, msg)
	}
	return fmt.Errorf("the catalogue answered %d: %s", status, msg)
}

var (
	compileOnce sync.Once
	schemas     map[string]*jsonschema.Schema
	compileErr  error
)

// decode checks an answer against the schema's definition at ref, then reads
// it into v.
func decode(data []byte, ref string, v any) error {
	if err := conforms(data, ref); err != nil {
		var verr *jsonschema.ValidationError
		if errors.As(err, &verr) {
			return fmt.Errorf("the catalogue answered outside its contract: %v", verr)
		}
		return err
	}
	return json.Unmarshal(data, v)
}

// conforms checks JSON against the catalogue schema's definition at ref.
func conforms(data []byte, ref string) error {
	compileOnce.Do(func() {
		c := jsonschema.NewCompiler()
		for url, doc := range map[string][]byte{spec.RecipeV0URL: spec.RecipeV0, spec.CatalogueV0URL: spec.CatalogueV0} {
			parsed, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
			if err != nil {
				compileErr = err
				return
			}
			if err := c.AddResource(url, parsed); err != nil {
				compileErr = err
				return
			}
		}
		schemas = map[string]*jsonschema.Schema{}
		for _, def := range []string{"#/$defs/project", "#/$defs/search", "#/$defs/run"} {
			if schemas[def], compileErr = c.Compile(spec.CatalogueV0URL + def); compileErr != nil {
				return
			}
		}
	})
	if compileErr != nil {
		return fmt.Errorf("the catalogue's schema: %w", compileErr)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("not JSON: %w", err)
	}
	return schemas[ref].Validate(inst)
}
