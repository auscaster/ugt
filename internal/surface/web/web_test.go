package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"time"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/recipe"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// shelf is a catalogue of one source.
type shelf struct {
	source  catalogue.Source
	entries []catalogue.Entry
}

func (s shelf) Source() catalogue.Source { return s.source }
func (s shelf) Project(_ context.Context, owner, repo string) ([]catalogue.Entry, error) {
	var out []catalogue.Entry
	for _, e := range s.entries {
		if strings.HasPrefix(e.Recipe, owner+"/"+repo+"/") {
			src, err := s.Open(context.Background(), e.Recipe)
			if err != nil {
				return nil, err
			}
			e.Revision, err = recipe.Revision(src)
			if err != nil {
				return nil, err
			}
			out = append(out, e)
		}
	}
	return out, nil
}
func (shelf) Search(context.Context, []string) ([]catalogue.Entry, error) { return nil, nil }
func (shelf) Open(context.Context, string) (fs.FS, error) {
	return os.DirFS("../../../spec/recipe/v0/examples/valid/example-job"), nil
}

// readDisk reads the real disk and writes nothing.
type readDisk struct{}

func (readDisk) TryLock(string) (func(), error) { return nil, fs.ErrPermission }

func (readDisk) FS(dir string) fs.FS            { return os.DirFS(dir) }
func (readDisk) WriteFile(string, []byte) error { return fs.ErrPermission }
func (readDisk) RemoveAll(string) error         { return fs.ErrPermission }
func (readDisk) Exists(p string) (bool, error)  { _, err := os.Stat(p); return err == nil, nil }

var published = shelf{catalogue.Published, []catalogue.Entry{{
	Recipe: "example/tool/markdown", What: "turn a spreadsheet into a report", Kind: "job", Source: catalogue.Published,
	Inputs: []recipe.File{{Path: "inputs/data.csv", What: "the spreadsheet"}},
	Access: []recipe.Access{{Kind: "internet", Why: "it fetches exchange rates"}},
}}}

var local = shelf{catalogue.Local, []catalogue.Entry{{Recipe: "example/tool/mine", What: "something made here", Kind: "job", Source: catalogue.Local}}}

// serve starts the page for link and returns its address, and a channel that
// closes when it stops.
func serve(t *testing.T, link string, idle time.Duration, runtimes ...app.Runtime) (string, chan struct{}) {
	t.Helper()
	a := &app.App{Catalogues: []app.Catalogue{local, published}, Disk: readDisk{}, Layout: workspace.Layout{Home: t.TempDir(), State: t.TempDir()}, Now: time.Now}
	if len(runtimes) > 0 {
		a.Runtime = runtimes[0]
	}
	opened := make(chan string, 1)
	s := &Server{App: a, Link: link, Idle: idle, Opened: func(u string) error { opened <- u; return nil }}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { _ = s.Serve(ctx); close(stopped) }()
	t.Cleanup(func() { cancel(); <-stopped })
	return <-opened, stopped
}

func get(t *testing.T, u string, v any) *http.Response {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if v != nil {
		_ = json.NewDecoder(resp.Body).Decode(v)
	}
	return resp
}

// The page offers a link's catalogue recipes, with what they reach in plain
// words, and never one made elsewhere.
func TestPageOffersOnlyCatalogueRecipes(t *testing.T) {
	page, _ := serve(t, "ugt://run/example/tool", time.Hour)
	var p plan
	get(t, page+"api/plan", &p)
	if len(p.Recipes) != 1 || p.Recipes[0].Recipe != "example/tool/markdown" || p.Error != "" {
		t.Fatalf("%+v", p)
	}
	if a := p.Recipes[0].Access; len(a) != 1 || a[0].What != "Reach the internet" || a[0].Why != "it fetches exchange rates" {
		t.Errorf("access %+v", a)
	}

	page, _ = serve(t, "ugt://run/example/tool/mine", time.Hour)
	p = plan{}
	get(t, page+"api/plan", &p)
	if len(p.Recipes) != 0 || !strings.Contains(p.Error, "the catalogue has no recipe for example/tool/mine") {
		t.Errorf("a recipe made here: %+v", p)
	}
	page, _ = serve(t, "ugt://exec/example/tool", time.Hour)
	p = plan{}
	get(t, page+"api/plan", &p)
	if !strings.Contains(p.Error, "is not a link UGT opens") {
		t.Errorf("a link that is not one: %+v", p)
	}
}

// Only the page itself reaches it: at its own address, with its secret, and
// for a change, from its own origin and for a recipe it offered.
func TestOnlyThePageReachesIt(t *testing.T) {
	page, _ := serve(t, "ugt://run/example/tool", time.Hour)
	u, _ := url.Parse(page)

	resp := get(t, page, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("the page: %s %v", resp.Status, resp.Header)
	}
	if resp := get(t, "http://"+u.Host+"/api/plan", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("without the secret: %s", resp.Status)
	}

	rebound, _ := http.NewRequest(http.MethodGet, page+"api/plan", nil)
	rebound.Host = "attacker.example:" + u.Port()
	if resp, err := http.DefaultClient.Do(rebound); err != nil || resp.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("another site's name for 127.0.0.1: %v %v", resp.Status, err)
	}

	post := func(origin, recipe string) (int, string) {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		_ = form.WriteField("recipe", recipe)
		_ = form.Close()
		req, _ := http.NewRequest(http.MethodPost, page+"api/run", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, _ := post("https://attacker.example", "example/tool/markdown"); code != http.StatusForbidden {
		t.Errorf("a run from another site: %d", code)
	}
	if code, _ := post("", "example/tool/markdown"); code != http.StatusForbidden {
		t.Errorf("a run from no page: %d", code)
	}
	if code, body := post("http://"+u.Host, "example/tool/mine"); code != http.StatusBadRequest || !strings.Contains(body, "not a recipe this page offers") {
		t.Errorf("a recipe the page did not offer: %d %s", code, body)
	}
	if resp := get(t, page+"api/job?id=20261102-000000-0000", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("a job the page did not start: %s", resp.Status)
	}
}

// The page stops once it has gone quiet with nothing of its running.
func TestStopsOnceQuiet(t *testing.T) {
	_, stopped := serve(t, "ugt://run/example/tool", time.Millisecond)
	<-stopped
}

// The page's wordmarks are the repository's own.
func TestWordmarksAreTheRepositorys(t *testing.T) {
	for _, name := range []string{"ugt-wordmark-black.svg", "ugt-wordmark-white.svg"} {
		ours, _ := static.ReadFile("static/" + name)
		theirs, err := os.ReadFile("../../../assets/" + name)
		if err != nil || !bytes.Equal(ours, theirs) {
			t.Errorf("static/%s is not assets/%s: %v", name, name, err)
		}
	}
}

// A visitor can finish setting up after a Windows restart from the same
// project. Only an explicit, same-page confirmation can start that operation.
type restartRuntime struct {
	app.Runtime
	attempts int
}

func (*restartRuntime) Name() string { return "wsl" }
func (rt *restartRuntime) Setup(_ context.Context, opts app.SetupOptions) (app.SetupResult, error) {
	if !opts.AllowAdmin {
		return app.SetupResult{}, fmt.Errorf("no consent")
	}
	rt.attempts++
	return app.SetupResult{NeedsRestart: rt.attempts == 1, Ready: rt.attempts == 2}, nil
}
func TestPageResumesSetupAfterRestart(t *testing.T) {
	rt := &restartRuntime{}
	page, _ := serve(t, "ugt://run/example/tool", time.Hour, rt)
	host, _ := url.Parse(page)
	submit := func(origin, agree string) (int, string) {
		body := url.Values{"recipe": {"example/tool/markdown"}, "agree": {agree}}
		req, _ := http.NewRequest("POST", page+"api/setup", strings.NewReader(body.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var result map[string]string
		_ = json.NewDecoder(res.Body).Decode(&result)
		return res.StatusCode, result["job"]
	}
	origin := "http://" + host.Host
	if code, _ := submit("https://example.com", "yes"); code != 403 {
		t.Fatal(code)
	}
	if code, _ := submit(origin, ""); code != 400 {
		t.Fatal(code)
	}
	for i := 0; i < 2; i++ {
		code, id := submit(origin, "yes")
		if code != 200 || id == "" {
			t.Fatalf("%d %q", code, id)
		}
		var plan plan
		get(t, page+"api/plan", &plan)
		if !plan.WindowsSetup || plan.LastJob != id {
			t.Fatalf("reload did not recover setup: %+v", plan)
		}
		var j struct {
			State  string
			Result app.SetupResult
		}
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
			get(t, page+"api/job?id="+id, &j)
			if j.State == "succeeded" {
				break
			}
		}
		if j.State != "succeeded" || j.Result.NeedsRestart != (i == 0) || j.Result.Ready != (i == 1) {
			t.Fatalf("setup %d: %+v", i, j)
		}
	}
	if rt.attempts != 2 {
		t.Fatalf("reload started another attempt: %d", rt.attempts)
	}
}
