package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

// fakeRepo serves a catalogue holding the spec's example job as
// example/tool/markdown, the way GitHub's API and raw host do, counting calls.
type fakeRepo struct {
	head        string
	files       map[string][]byte // by path inside the recipe folder
	tamper      bool              // serve one file with the wrong content
	calls       map[string]int
	notModified int
}

func (f *fakeRepo) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		f.calls[key]++
		switch {
		case key == "/repos/auscaster/ugt/commits/main":
			if r.Header.Get("If-None-Match") == `"`+f.head+`"` {
				f.notModified++
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"`+f.head+`"`)
			fmt.Fprint(w, f.head)
		case key == "/repos/auscaster/ugt/contents/recipes/example/tool":
			fmt.Fprint(w, `[{"name": "markdown", "type": "dir", "sha": "tree1"}]`)
		case key == "/repos/auscaster/ugt/contents/recipes/someone/else":
			w.WriteHeader(http.StatusNotFound)
		case key == "/repos/auscaster/ugt/git/trees/tree1":
			var entries []map[string]string
			for name, data := range f.files {
				entries = append(entries, map[string]string{"path": name, "type": "blob", "sha": blobSHA(data), "mode": "100644"})
			}
			json.NewEncoder(w).Encode(map[string]any{"tree": entries})
		case strings.HasPrefix(key, "/auscaster/ugt/"+f.head+"/recipes/example/tool/markdown/"):
			name := strings.TrimPrefix(key, "/auscaster/ugt/"+f.head+"/recipes/example/tool/markdown/")
			data := f.files[name]
			if f.tamper && name == "compose.yaml" {
				data = []byte("services: {evil: {image: x}}\n")
			}
			w.Write(data)
		default:
			t.Errorf("unexpected %s", key)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func newFake(t *testing.T) (*fakeRepo, *Catalogue, *httptest.Server) {
	t.Helper()
	files := map[string][]byte{}
	for _, name := range []string{"recipe.json", "compose.yaml"} {
		data, err := os.ReadFile("../../../spec/recipe/v0/examples/valid/example-job/" + name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = data
	}
	f := &fakeRepo{head: strings.Repeat("a", 40), files: files, calls: map[string]int{}}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	client := New(func() string { return "" })
	client.API, client.Raw = srv.URL, srv.URL
	return f, NewCatalogue(client, "auscaster/ugt", "recipes", "main", t.TempDir()), srv
}

func TestCatalogueProject(t *testing.T) {
	f, c, _ := newFake(t)
	ctx := context.Background()
	entries, err := c.Project(ctx, "Example", "Tool")
	if err != nil || len(entries) != 1 || entries[0].Recipe != "example/tool/markdown" || entries[0].Problem != "" || entries[0].Source != catalogue.Published {
		t.Fatalf("project %+v, %v", entries, err)
	}
	if none, err := c.Project(ctx, "someone", "else"); err != nil || len(none) != 0 {
		t.Errorf("a project with no recipes: %v, %v", none, err)
	}
	fsys, err := c.Open(ctx, "example/tool/markdown")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recipe.Load(fsys); err != nil {
		t.Errorf("opened recipe: %v", err)
	}
	// the head is trusted for a minute, and a recipe folder is fetched once
	if f.calls["/repos/auscaster/ugt/commits/main"] != 1 || f.calls["/repos/auscaster/ugt/git/trees/tree1"] != 1 || f.calls["/auscaster/ugt/"+f.head+"/recipes/example/tool/markdown/compose.yaml"] != 1 {
		t.Errorf("calls %v", f.calls)
	}
	// later, an unmoved head costs a conditional request and nothing else
	c.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if _, err := c.Project(ctx, "example", "tool"); err != nil {
		t.Fatal(err)
	}
	if f.notModified != 1 || f.calls["/repos/auscaster/ugt/git/trees/tree1"] != 1 {
		t.Errorf("an unmoved head refetched: %d not modified, calls %v", f.notModified, f.calls)
	}
}

func TestCatalogueRefusesTamperedFiles(t *testing.T) {
	f, c, _ := newFake(t)
	f.tamper = true
	if _, err := c.Project(context.Background(), "example", "tool"); err == nil || !strings.Contains(err.Error(), "not using it") {
		t.Fatalf("a file that does not match its hash was used: %v", err)
	}
}

func TestCatalogueWorksOffline(t *testing.T) {
	_, c, srv := newFake(t)
	ctx := context.Background()
	if _, err := c.Project(ctx, "example", "tool"); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	c.now = func() time.Time { return time.Now().Add(time.Hour) }
	entries, err := c.Project(ctx, "example", "tool")
	if err != nil || len(entries) != 1 {
		t.Fatalf("offline: %+v, %v", entries, err)
	}
}
