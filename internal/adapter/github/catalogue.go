package github

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/catalogue"
)

// Catalogue is the published catalogue, read live from its repository, so a
// recipe merged there is in use at once, with no release of ugt. It asks
// about one project at a time and keeps what it fetched by content: a recipe
// folder by its Git tree hash and each file by its blob hash, both checked as
// they arrive and never fetched again. The branch's head is asked with a
// conditional request, which costs nothing against GitHub's rate limit when
// it has not moved, and trusted for a minute. Offline, the last head known and
// the folders already fetched still work.
type Catalogue struct {
	client      *Client
	repo        string // owner/name
	owner, name string
	dir         string // the catalogue's folder in the repository
	branch      string
	cache       string // a folder of its own
	headTTL     time.Duration
	now         func() time.Time

	mu   sync.Mutex
	head head
}

type head struct {
	SHA     string    `json:"sha"`
	ETag    string    `json:"etag"`
	Checked time.Time `json:"checked"`
}

var _ app.Catalogue = (*Catalogue)(nil)

// NewCatalogue reads the catalogue in dir of repo's branch, keeping what it
// fetches in cache.
func NewCatalogue(client *Client, repo, dir, branch, cache string) *Catalogue {
	owner, name, _ := strings.Cut(repo, "/")
	return &Catalogue{client: client, repo: repo, owner: owner, name: name, dir: dir, branch: branch, cache: cache,
		headTTL: time.Minute, now: time.Now}
}

func (c *Catalogue) Source() catalogue.Source { return catalogue.Published }

// Project lists the recipes for owner/repo: one listing of its folder.
func (c *Catalogue) Project(ctx context.Context, owner, repo string) ([]catalogue.Entry, error) {
	commit, err := c.headSHA(ctx)
	if err != nil {
		return nil, err
	}
	project := strings.ToLower(owner + "/" + repo)
	items, err := c.list(ctx, commit, path.Join(c.dir, project))
	if err != nil {
		return nil, err
	}
	var out []catalogue.Entry
	for _, it := range items {
		if it.Type != "dir" {
			continue
		}
		id := project + "/" + it.Name
		folder, err := c.folder(ctx, commit, id, it.SHA)
		if err != nil {
			return nil, err
		}
		out = append(out, catalogue.FromFolder(os.DirFS(folder), id, catalogue.Published))
	}
	return out, nil
}

// searchLimit bounds how many matching recipes a search describes.
const searchLimit = 20

// Search lists the recipes whose ID holds every word, from the catalogue's
// tree at the head, describing at most searchLimit of them.
func (c *Catalogue) Search(ctx context.Context, words []string) ([]catalogue.Entry, error) {
	commit, err := c.headSHA(ctx)
	if err != nil {
		return nil, err
	}
	tree, err := c.tree(ctx, commit)
	if err != nil {
		return nil, err
	}
	var out []catalogue.Entry
	for _, e := range tree {
		id, ok := strings.CutPrefix(e.Path, c.dir+"/")
		if !ok || e.Type != "tree" || strings.Count(id, "/") != 2 {
			continue
		}
		match := len(words) > 0
		for _, w := range words {
			match = match && strings.Contains(id, strings.ToLower(w))
		}
		if !match {
			continue
		}
		folder, err := c.folder(ctx, commit, id, e.SHA)
		if err != nil {
			return nil, err
		}
		out = append(out, catalogue.FromFolder(os.DirFS(folder), id, catalogue.Published))
		if len(out) == searchLimit {
			break
		}
	}
	return out, nil
}

// Open returns a recipe's folder at the head.
func (c *Catalogue) Open(ctx context.Context, id string) (fs.FS, error) {
	commit, err := c.headSHA(ctx)
	if err != nil {
		return nil, err
	}
	project, task := path.Split(id)
	items, err := c.list(ctx, commit, path.Join(c.dir, strings.TrimSuffix(project, "/")))
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.Type == "dir" && it.Name == task {
			folder, err := c.folder(ctx, commit, id, it.SHA)
			if err != nil {
				return nil, err
			}
			return os.DirFS(folder), nil
		}
	}
	return nil, fmt.Errorf("%s in the catalogue: %w", id, app.ErrNotFound)
}

// headSHA is the branch's head commit.
func (c *Catalogue) headSHA(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	file := filepath.Join(c.cache, "head.json")
	if c.head.SHA == "" {
		if data, err := os.ReadFile(file); err == nil {
			_ = json.Unmarshal(data, &c.head)
		}
	}
	if c.head.SHA != "" && c.now().Sub(c.head.Checked) < c.headTTL {
		return c.head.SHA, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/commits/%s", c.client.API, c.repo, c.branch), nil)
	if err != nil {
		return "", err
	}
	c.client.authorize(req, "application/vnd.github.sha")
	if c.head.ETag != "" {
		req.Header.Set("If-None-Match", c.head.ETag)
	}
	resp, err := c.client.HTTP.Do(req)
	if err != nil {
		if c.head.SHA != "" {
			return c.head.SHA, nil // offline: the last head known
		}
		return "", fmt.Errorf("could not reach the catalogue on GitHub: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotModified:
	case http.StatusOK:
		body, err := io.ReadAll(io.LimitReader(resp.Body, 100))
		if err != nil {
			return "", err
		}
		sha := strings.TrimSpace(string(body))
		if len(sha) != 40 {
			return "", fmt.Errorf("GitHub answered %q for the catalogue's head", sha)
		}
		c.head.SHA, c.head.ETag = sha, resp.Header.Get("ETag")
	default:
		if c.head.SHA != "" {
			return c.head.SHA, nil
		}
		return "", fmt.Errorf("GitHub answered %s for the catalogue's head", resp.Status)
	}
	c.head.Checked = c.now()
	if data, err := json.Marshal(c.head); err == nil {
		_ = os.MkdirAll(c.cache, 0o755)
		_ = os.WriteFile(file, data, 0o644)
	}
	return c.head.SHA, nil
}

type item struct {
	Name string `json:"name"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// list returns a folder's entries at a commit; a folder that is not there
// has none. Listings are kept by commit, which never changes.
func (c *Catalogue) list(ctx context.Context, commit, dir string) ([]item, error) {
	file := filepath.Join(c.cache, "lists", commit, filepath.FromSlash(dir)+".json")
	var items []item
	if data, err := os.ReadFile(file); err == nil && json.Unmarshal(data, &items) == nil {
		return items, nil
	}
	err := c.client.getJSON(ctx, fmt.Sprintf("/repos/%s/contents/%s?ref=%s", c.repo, escapePath(dir), commit), dir+" in the catalogue", &items)
	if errors.Is(err, app.ErrNotFound) {
		items, err = []item{}, nil
	}
	if err != nil {
		return nil, err
	}
	return items, keep(file, items)
}

type treeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Mode string `json:"mode"`
}

// tree returns the whole repository's tree at a commit, kept by commit.
func (c *Catalogue) tree(ctx context.Context, commit string) ([]treeEntry, error) {
	file := filepath.Join(c.cache, "trees", commit+".json")
	var entries []treeEntry
	if data, err := os.ReadFile(file); err == nil && json.Unmarshal(data, &entries) == nil {
		return entries, nil
	}
	var body struct {
		Tree      []treeEntry `json:"tree"`
		Truncated bool        `json:"truncated"`
	}
	if err := c.client.getJSON(ctx, fmt.Sprintf("/repos/%s/git/trees/%s?recursive=1", c.repo, commit), "the catalogue's tree", &body); err != nil {
		return nil, err
	}
	return body.Tree, keep(file, body.Tree)
}

// folder makes sure a recipe folder is in the cache, by its tree hash, and
// returns where. Each file is checked against its blob hash as it arrives.
func (c *Catalogue) folder(ctx context.Context, commit, id, treeSHA string) (string, error) {
	dest := filepath.Join(c.cache, "recipes", treeSHA)
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	var tree struct {
		Tree []treeEntry `json:"tree"`
	}
	if err := c.client.getJSON(ctx, fmt.Sprintf("/repos/%s/git/trees/%s?recursive=1", c.repo, treeSHA), id+" in the catalogue", &tree); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), treeSHA+".*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	for _, e := range tree.Tree {
		if e.Type != "blob" {
			continue
		}
		data, err := c.client.File(ctx, c.owner, c.name, commit, path.Join(c.dir, id, e.Path))
		if err != nil {
			return "", err
		}
		if got := blobSHA(data); got != e.SHA {
			return "", fmt.Errorf("%s/%s arrived with Git hash %s, not %s: not using it", id, e.Path, got, e.SHA)
		}
		target := filepath.Join(tmp, filepath.FromSlash(e.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return "", err
		}
	}
	// complete or not at all: another process may have finished it first
	if err := os.Rename(tmp, dest); err != nil {
		if _, serr := os.Stat(dest); serr != nil {
			return "", err
		}
	}
	return dest, nil
}

// blobSHA is Git's hash of a file's content.
func blobSHA(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// keep writes v to file as JSON, creating its folder.
func keep(file string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, data, 0o644)
}
