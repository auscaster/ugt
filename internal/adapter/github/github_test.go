package github

import (
	"context"
	"errors"
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
	c := New("tok")
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
	c, ctx := New(os.Getenv("GITHUB_TOKEN")), context.Background()
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
