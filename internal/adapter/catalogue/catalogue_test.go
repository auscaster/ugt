package catalogue

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/admission"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

const exampleJob = "../../../spec/recipe/v0/examples/valid/example-job"

// service is a fake catalogue service holding the example job.
type service struct {
	*httptest.Server
	blobs    map[string][]byte
	project  []byte
	requests atomic.Int32
	token    string // the login it asks for, when set
	corrupt  bool   // serve one blob wrong
}

func newService(t *testing.T) *service {
	t.Helper()
	s := &service{blobs: map[string][]byte{}}
	var files []file
	err := fs.WalkDir(os.DirFS(exampleJob), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(filepath.Join(exampleJob, p))
		if err != nil {
			return err
		}
		sha := admission.BlobSHA(data)
		s.blobs[sha] = data
		files = append(files, file{Path: p, Mode: "100644", SHA: sha, Size: int64(len(data))})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f, err := recipe.Load(os.DirFS(exampleJob))
	if err != nil {
		t.Fatal(err)
	}
	rev, _ := recipe.Revision(os.DirFS(exampleJob))
	r := map[string]any{
		"recipe": "example/tool/markdown", "revision": rev, "task": f.Recipe.Task, "kind": f.Recipe.Kind, "repo": f.Recipe.Repo,
		"commit": f.Recipe.Commit, "licence": f.Recipe.Licence, "inputs": f.Recipe.Inputs, "outputs": f.Recipe.Outputs, "files": files,
		"images":   map[string]any{"": map[string]any{"ref": "ghcr.io/auscaster/ugt/recipes:" + rev, "digest": "sha256:" + strings.Repeat("a", 64), "arches": []string{"amd64", "arm64"}}},
		"verified": []map[string]any{{"arch": "arm64", "ugt": strings.Repeat("b", 40), "seconds": 12.5, "at": "2026-10-30T05:00:00Z"}},
	}
	s.project, _ = json.Marshal(map[string]any{"project": "example/tool", "recipes": []any{r}})
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		s.requests.Add(1)
		if s.token != "" && req.Header.Get("Authorization") != "Bearer "+s.token {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, `{"error": "sign in to GitHub with ugt login: the catalogue is private"}`, http.StatusUnauthorized)
			return
		}
		switch p := req.URL.Path; {
		case p == "/v1/projects/example/tool":
			if req.Header.Get("If-None-Match") == `"1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"1"`)
			w.Write(s.project)
		case p == "/v1/projects/nobody/here":
			w.Write([]byte(`{"project": "nobody/here", "recipes": []}`))
		case p == "/v1/search":
			if req.URL.Query().Get("q") != "convert word" {
				t.Errorf("search %q", req.URL.RawQuery)
			}
			w.Write([]byte(`{"query": "convert word", "recipes": [` + string(mustRecipe(s.project)) + `]}`))
		case strings.HasPrefix(p, "/v1/blobs/"):
			data, ok := s.blobs[strings.TrimPrefix(p, "/v1/blobs/")]
			if !ok {
				http.NotFound(w, req)
				return
			}
			if s.corrupt {
				data = append([]byte("tampered "), data...)
			}
			w.Write(data)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func mustRecipe(project []byte) json.RawMessage {
	var p struct {
		Recipes []json.RawMessage `json:"recipes"`
	}
	_ = json.Unmarshal(project, &p)
	return p.Recipes[0]
}

func client(t *testing.T, s *service, token string) (*Client, *time.Time) {
	now := time.Date(2026, 10, 30, 6, 0, 0, 0, time.UTC)
	c := New(s.URL+"/v1/", func() string { return token }, t.TempDir())
	c.now = func() time.Time { return now }
	return c, &now
}

func TestProjectIsOneCallKeptForAMinute(t *testing.T) {
	s := newService(t)
	c, now := client(t, s, "")
	ctx := context.Background()
	got, err := c.Project(ctx, "Example", "Tool")
	if err != nil || len(got) != 1 {
		t.Fatalf("project %+v, %v", got, err)
	}
	e := got[0]
	if e.Recipe != "example/tool/markdown" || e.Images[""].Digest == "" || len(e.Verified) != 1 || e.Source != "catalogue" || len(e.Inputs) == 0 {
		t.Errorf("entry %+v", e)
	}
	if _, err := c.Project(ctx, "example", "tool"); err != nil || s.requests.Load() != 1 {
		t.Fatalf("within a minute: %v, %d requests", err, s.requests.Load())
	}
	*now = now.Add(2 * time.Minute)
	if _, err := c.Project(ctx, "example", "tool"); err != nil || s.requests.Load() != 2 {
		t.Fatalf("after a minute, a conditional request: %v, %d requests", err, s.requests.Load())
	}
	// one answer kept per project, replaced in place
	kept, _ := filepath.Glob(filepath.Join(c.cache, "projects", "*", "*"))
	if len(kept) != 1 {
		t.Errorf("kept %v", kept)
	}
	if none, err := c.Project(ctx, "nobody", "here"); err != nil || len(none) != 0 {
		t.Errorf("a project with no recipe: %v, %v", none, err)
	}
}

func TestOfflineUsesTheLastAnswer(t *testing.T) {
	s := newService(t)
	c, now := client(t, s, "")
	ctx := context.Background()
	if _, err := c.Open(ctx, "example/tool/markdown"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	*now = now.Add(time.Hour)
	src, err := c.Open(ctx, "example/tool/markdown")
	if err != nil {
		t.Fatalf("offline: %v", err)
	}
	if _, err := fs.ReadFile(src, recipe.RecordFile); err != nil {
		t.Error(err)
	}
	if _, err := c.Project(ctx, "other", "project"); err == nil || !strings.Contains(err.Error(), "could not reach the catalogue") {
		t.Errorf("never asked, offline: %v", err)
	}
}

func TestOpenChecksEveryFileAndTheRevision(t *testing.T) {
	s := newService(t)
	c, _ := client(t, s, "")
	ctx := context.Background()
	src, err := c.Open(ctx, "example/tool/markdown")
	if err != nil {
		t.Fatal(err)
	}
	if rev, _ := recipe.Revision(src); !strings.Contains(string(s.project), `"revision":"`+rev+`"`) {
		t.Errorf("revision %s", rev)
	}
	before := s.requests.Load()
	if _, err := c.Open(ctx, "example/tool/markdown"); err != nil || s.requests.Load() != before {
		t.Errorf("a kept folder was fetched again: %v", err)
	}

	tampered := newService(t)
	tampered.corrupt = true
	c, _ = client(t, tampered, "")
	if _, err := c.Open(ctx, "example/tool/markdown"); err == nil || !strings.Contains(err.Error(), "not using it") {
		t.Fatalf("tampered: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(c.cache, "recipes", "*")); len(left) != 0 {
		t.Errorf("a half-fetched folder was kept: %v", left)
	}
	if _, err := c.Open(ctx, "example/tool/other"); !errors.Is(err, app.ErrNotFound) {
		t.Errorf("unknown recipe: %v", err)
	}
}

// While the catalogue is private, the service asks for a GitHub login; the
// client sends the user's only then.
func TestLoginOnlyWhenAsked(t *testing.T) {
	s := newService(t)
	s.token = "gho_user"
	c, _ := client(t, s, "")
	if _, err := c.Project(context.Background(), "example", "tool"); err == nil || !strings.Contains(err.Error(), "ugt login") {
		t.Fatalf("no login: %v", err)
	}
	c, _ = client(t, s, "gho_user")
	if _, err := c.Project(context.Background(), "example", "tool"); err != nil {
		t.Fatalf("with a login: %v", err)
	}
}

func TestSearch(t *testing.T) {
	s := newService(t)
	c, _ := client(t, s, "")
	got, err := c.Search(context.Background(), []string{"convert", "word"})
	if err != nil || len(got) != 1 || got[0].Recipe != "example/tool/markdown" {
		t.Fatalf("search %+v, %v", got, err)
	}
}

func TestAnswersOutsideTheContractAreRefused(t *testing.T) {
	s := newService(t)
	s.project = []byte(`{"project": "example/tool", "recipes": [{"recipe": "example/tool/markdown"}]}`)
	c, _ := client(t, s, "")
	if _, err := c.Project(context.Background(), "example", "tool"); err == nil || !strings.Contains(err.Error(), "outside its contract") {
		t.Fatalf("got %v", err)
	}
}
