package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/app"
)

const sha = "86098128c0b4f24f0e2aa2994de830614b474227"

// fake answers like GitHub's API and raw host for one repository.
func fake(t *testing.T) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/openai/whisper", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("token not sent: %v", r.Header)
		}
		w.Write([]byte(`{"name": "whisper", "owner": {"login": "openai"}, "default_branch": "main",
			"language": "Python", "license": {"spdx_id": "MIT"}}`))
	})
	mux.HandleFunc("/repos/openai/whisper/commits/main", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github.sha" {
			t.Errorf("accept %q", r.Header.Get("Accept"))
		}
		w.Write([]byte(sha))
	})
	mux.HandleFunc("/repos/openai/whisper/contents/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != sha {
			t.Errorf("ref %q", r.URL.Query().Get("ref"))
		}
		w.Write([]byte(`[{"path": "docker-compose.yml", "type": "file"}, {"path": "whisper", "type": "dir"}]`))
	})
	mux.HandleFunc("/openai/whisper/"+sha+"/docker-compose.yml", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("services: {}\n"))
	})
	mux.HandleFunc("/repos/limited/repo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1793000000")
		w.WriteHeader(http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := New(func() string { return "tok" })
	c.API, c.Raw = srv.URL, srv.URL
	return c
}

func TestRepoCommitListFile(t *testing.T) {
	c, ctx := fake(t), context.Background()
	repo, err := c.Repo(ctx, "openai", "whisper")
	if err != nil {
		t.Fatal(err)
	}
	if repo != (app.Repo{Owner: "openai", Name: "whisper", DefaultBranch: "main", Licence: "MIT", Language: "Python"}) {
		t.Errorf("repo %+v", repo)
	}
	commit, err := c.Commit(ctx, "openai", "whisper", "main")
	if err != nil || commit != sha {
		t.Fatalf("commit %q, %v", commit, err)
	}
	entries, err := c.List(ctx, "openai", "whisper", sha, "")
	if err != nil || len(entries) != 2 || entries[1] != (app.Entry{Path: "whisper", Dir: true}) {
		t.Fatalf("entries %+v, %v", entries, err)
	}
	data, err := c.File(ctx, "openai", "whisper", sha, "docker-compose.yml")
	if err != nil || string(data) != "services: {}\n" {
		t.Fatalf("file %q, %v", data, err)
	}
}

func TestErrors(t *testing.T) {
	c, ctx := fake(t), context.Background()
	if _, err := c.Repo(ctx, "no", "such"); !errors.Is(err, app.ErrNotFound) {
		t.Errorf("missing repo: %v", err)
	}
	if _, err := c.File(ctx, "openai", "whisper", sha, ".env"); !errors.Is(err, app.ErrNotFound) {
		t.Errorf("missing file: %v", err)
	}
	if _, err := c.Repo(ctx, "limited", "repo"); err == nil || !strings.Contains(err.Error(), "set GITHUB_TOKEN") {
		t.Errorf("rate limit: %v", err)
	}
}

// Contract: against github.com, a public repository resolves. Skipped in
// short mode and offline.
func TestRealGitHub(t *testing.T) {
	if testing.Short() || os.Getenv("UGT_OFFLINE") != "" {
		t.Skip("contract test needs github.com")
	}
	c, ctx := New(func() string { return os.Getenv("GITHUB_TOKEN") }), context.Background()
	repo, err := c.Repo(ctx, "openai", "whisper")
	if err != nil {
		t.Skipf("github.com unreachable: %v", err)
	}
	if repo.Licence != "MIT" || repo.DefaultBranch == "" {
		t.Errorf("repo %+v", repo)
	}
	data, err := c.File(ctx, "openai", "whisper", sha, "LICENSE")
	if err != nil || !strings.Contains(string(data), "MIT License") {
		t.Errorf("LICENSE at %s: %v", sha, err)
	}
}

// fakeAPI answers the Git data API calls Propose makes, and records them.
type fakeAPI struct {
	push   bool
	calls  []string
	tree   map[string]any // the last tree written
	prOpen bool
}

func (f *fakeAPI) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("%s %s without the token", r.Method, r.URL.Path)
		}
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		reply := func(v string) { w.Write([]byte(v)) }
		switch {
		case r.URL.Path == "/repos/auscaster/ugt" && r.Method == "GET":
			reply(fmt.Sprintf(`{"default_branch": "main", "permissions": {"push": %v}}`, f.push))
		case r.URL.Path == "/user":
			reply(`{"login": "someone"}`)
		case r.URL.Path == "/repos/auscaster/ugt/forks":
			reply(`{"full_name": "someone/ugt"}`)
		case r.URL.Path == "/repos/someone/ugt":
			reply(`{}`)
		case r.URL.Path == "/repos/auscaster/ugt/git/ref/heads/main":
			reply(`{"object": {"sha": "base"}}`)
		case r.URL.Path == "/repos/auscaster/ugt/git/commits/base":
			reply(`{"tree": {"sha": "basetree"}}`)
		case r.URL.Path == "/repos/auscaster/ugt/git/trees/basetree":
			reply(`{"tree": [{"path": "recipes/a/b/c/old.sh", "type": "blob"}, {"path": "recipes/a/b/c/compose.yaml", "type": "blob"}, {"path": "README.md", "type": "blob"}]}`)
		case strings.HasSuffix(r.URL.Path, "/git/blobs"):
			reply(`{"sha": "blob"}`)
		case strings.HasSuffix(r.URL.Path, "/git/trees"):
			f.tree = body
			reply(`{"sha": "tree"}`)
		case strings.HasSuffix(r.URL.Path, "/git/commits"):
			reply(`{"sha": "commit"}`)
		case strings.HasSuffix(r.URL.Path, "/git/refs"):
			w.WriteHeader(http.StatusUnprocessableEntity) // the branch is there already
		case strings.Contains(r.URL.Path, "/git/refs/heads/"):
			reply(`{}`)
		case r.URL.Path == "/repos/auscaster/ugt/pulls" && r.Method == "POST":
			if f.prOpen {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			reply(`{"html_url": "https://github.com/auscaster/ugt/pull/7"}`)
		case r.URL.Path == "/repos/auscaster/ugt/pulls":
			reply(`[{"html_url": "https://github.com/auscaster/ugt/pull/7"}]`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestPropose(t *testing.T) {
	for _, push := range []bool{true, false} {
		f := &fakeAPI{push: push, prOpen: !push}
		srv := httptest.NewServer(f.handler(t))
		c := New(func() string { return "tok" })
		c.API = srv.URL
		url, err := c.Propose(context.Background(), app.Proposal{
			Repo: "auscaster/ugt", Dir: "recipes/a/b/c", Branch: "recipe/a-b-c",
			Files:   map[string][]byte{"compose.yaml": []byte("services: {}\n"), "recipe.json": []byte("{}")},
			Message: "feat(recipes): add a/b/c", Title: "Add a/b/c", Body: "verified",
		})
		srv.Close()
		if err != nil || url != "https://github.com/auscaster/ugt/pull/7" {
			t.Fatalf("push %v: %q, %v\n%s", push, url, err, strings.Join(f.calls, "\n"))
		}
		calls := strings.Join(f.calls, "\n")
		forked := strings.Contains(calls, "POST /repos/auscaster/ugt/forks")
		if forked == push || push != strings.Contains(calls, "POST /repos/auscaster/ugt/git/blobs") {
			t.Errorf("push %v: wrote to the wrong repository:\n%s", push, calls)
		}
		// the folder's old file goes; other files are untouched
		data, _ := json.Marshal(f.tree)
		if !strings.Contains(string(data), `"path":"recipes/a/b/c/old.sh","sha":null`) || strings.Contains(string(data), "README.md") {
			t.Errorf("tree %s", data)
		}
	}
	if _, err := New(func() string { return "" }).Propose(context.Background(), app.Proposal{}); !errors.Is(err, app.ErrNoLogin) {
		t.Errorf("without a login: %v", err)
	}
}
