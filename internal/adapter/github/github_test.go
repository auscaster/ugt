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
	"time"

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
	mux.HandleFunc("/repos/openai/whisper/license", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != sha {
			t.Errorf("licence ref %q", r.URL.Query().Get("ref"))
		}
		w.Write([]byte(`{"license":{"spdx_id":"MIT"}}`))
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
	c := New(func() string { return "tok" }, "")
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
	if licence, err := c.Licence(ctx, "openai", "whisper", sha); licence != "MIT" || err != nil {
		t.Fatalf("licence %q: %v", licence, err)
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
	c, ctx := New(func() string { return os.Getenv("GITHUB_TOKEN") }, ""), context.Background()
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
		case r.URL.Path == "/repos/auscaster/ugt/contents/recipes/a/b" && r.URL.Query().Get("ref") == "base":
			reply(`[{"name": "c", "type": "dir", "sha": "foldertree"}, {"name": "d", "type": "dir", "sha": "other"}]`)
		case r.URL.Path == "/repos/auscaster/ugt/git/trees/foldertree":
			reply(`{"tree": [{"path": "old.sh", "type": "blob"}, {"path": "compose.yaml", "type": "blob"}, {"path": "check", "type": "tree"}]}`)
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
		c := New(func() string { return "tok" }, "")
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
	if _, err := New(func() string { return "" }, "").Propose(context.Background(), app.Proposal{}); !errors.Is(err, app.ErrNoLogin) {
		t.Errorf("without a login: %v", err)
	}
}

// GitHub's device flow, as its documentation describes it: pending until the
// user approves, slowed when asked, then the token and who it belongs to.
func TestDeviceLogin(t *testing.T) {
	minPoll = time.Millisecond
	polls := 0
	outcome := "approve"
	mux := http.NewServeMux()
	mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("client_id") != "Iv1.ugt" || r.Form.Get("scope") != Scope {
			t.Errorf("device code form %v", r.Form)
		}
		w.Write([]byte(`{"device_code": "dev1", "user_code": "WDJB-MJHT", "verification_uri": "https://github.com/login/device", "expires_in": 900, "interval": 0}`))
	})
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("device_code") != "dev1" || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Errorf("token form %v", r.Form)
		}
		polls++
		switch {
		case polls == 1:
			w.Write([]byte(`{"error": "authorization_pending"}`))
		case polls == 2:
			w.Write([]byte(`{"error": "slow_down", "interval": 0}`))
		case outcome == "approve":
			w.Write([]byte(`{"access_token": "gho_user", "token_type": "bearer", "scope": "public_repo"}`))
		default:
			w.Write([]byte(`{"error": "` + outcome + `"}`))
		}
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gho_user" {
			t.Errorf("user read with %q", r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"login": "someone"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New(func() string { return "" }, "Iv1.ugt")
	c.API, c.Web = srv.URL, srv.URL
	ctx := context.Background()
	l, err := c.StartLogin(ctx)
	if err != nil || l.Code != "WDJB-MJHT" || l.URL != "https://github.com/login/device" {
		t.Fatalf("start %+v, %v", l, err)
	}
	token, user, err := c.FinishLogin(ctx, l)
	if err != nil || token != "gho_user" || user != "someone" || polls != 3 {
		t.Fatalf("finish %q %q %v after %d polls", token, user, err, polls)
	}
	for _, outcome = range []string{"expired_token", "access_denied"} {
		polls = 2
		if _, _, err := c.FinishLogin(ctx, l); err == nil {
			t.Errorf("%s: no error", outcome)
		}
	}
	if _, err := New(nil, "").StartLogin(ctx); err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("no client: %v", err)
	}
}
