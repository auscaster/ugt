// Package web is UGT's own page for an "Open in UGT" link from the web. A
// website cannot watch a program on the computer, so UGT serves the page on
// 127.0.0.1: it says what a catalogue recipe will run and with what access,
// takes the user's files, runs nothing until they press Run, then shows the
// progress and what the run gave them. The page's address carries a secret
// only UGT knows and the browser it opens, so no other page can read or drive
// it; it answers only to its own address and is never framed.
package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/usegeekthing/ugt/internal/app"
	"github.com/usegeekthing/ugt/internal/domain/access"
	"github.com/usegeekthing/ugt/internal/domain/catalogue"
	"github.com/usegeekthing/ugt/internal/domain/job"
	"github.com/usegeekthing/ugt/internal/domain/link"
	"github.com/usegeekthing/ugt/internal/domain/recipe"
)

//go:embed static
var static embed.FS

// Server serves the page for one link until it has gone quiet.
type Server struct {
	App  *app.App
	Link string // the link as the system passed it
	// Opened is given the page's address; by default the browser opens it.
	Opened func(url string) error
	// Idle is how long the page may be quiet, with nothing running, before
	// the server stops: the page asks every few seconds while it is open.
	Idle time.Duration

	planMu  sync.Mutex
	planned plan
	shown   []catalogue.Entry // the entries the plan shows, which are all a run may start

	mu      sync.Mutex
	last    time.Time
	lastJob string          // most recently accepted submission on this page, including across reloads
	jobs    map[string]bool // only the jobs this page started
}

// Serve runs the page until it has been quiet for Idle with no run going, or
// ctx ends.
func (s *Server) Serve(ctx context.Context) error {
	if s.Idle == 0 {
		s.Idle = 2 * time.Minute
	}
	s.jobs = map[string]bool{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	addr := ln.Addr().String()
	base := "/" + secret() + "/"
	files, _ := fs.Sub(static, "static")
	mux := http.NewServeMux()
	mux.Handle("GET "+base, http.StripPrefix(base, http.FileServerFS(files)))
	mux.HandleFunc("GET "+base+"api/plan", func(w http.ResponseWriter, r *http.Request) {
		p := s.plan(ctx)
		s.mu.Lock()
		p.LastJob = s.lastJob
		s.mu.Unlock()
		reply(w, p)
	})
	mux.HandleFunc("GET "+base+"api/alive", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST "+base+"api/run", func(w http.ResponseWriter, r *http.Request) { s.run(ctx, w, r) })
	mux.HandleFunc("POST "+base+"api/setup", func(w http.ResponseWriter, r *http.Request) { s.setup(ctx, w, r) })
	mux.HandleFunc("GET "+base+"api/job", s.job)
	mux.HandleFunc("POST "+base+"api/reveal", s.reveal)
	mux.HandleFunc("POST "+base+"api/stop", s.stop)
	srv := &http.Server{Handler: s.guard(addr, mux), ReadHeaderTimeout: 10 * time.Second}
	s.touch()
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	page := "http://" + addr + base
	open := s.Opened
	if open == nil {
		open = func(url string) error {
			if s.App.Browser == nil {
				return errors.New("nothing here opens a browser")
			}
			return s.App.Browser.Open(url)
		}
	}
	if err := open(page); err != nil {
		fmt.Fprintf(os.Stderr, "ugt: open %s in your browser: %v\n", page, err)
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if s.quiet() {
				return nil
			}
		}
	}
}

// secret is the part of the page's address no other page can guess.
func secret() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// guard answers only requests to the page's own address, so a page elsewhere
// that resolves its own name to 127.0.0.1 reaches nothing, and changes only
// for requests from the page itself.
func (s *Server) guard(addr string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != addr {
			http.Error(w, "this is not UGT's page", http.StatusMisdirectedRequest)
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("Origin") != "http://"+addr {
			http.Error(w, "only UGT's page can do that", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		s.touch()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) touch() {
	s.mu.Lock()
	s.last = time.Now()
	s.mu.Unlock()
}

// quiet is the page gone, or never opened, with no run of its going.
func (s *Server) quiet() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.jobs {
		if j, err := s.App.Job(id); err == nil && !j.State.Done() {
			return false
		}
	}
	return time.Since(s.last) >= s.Idle
}

// plan is what the page shows before anything runs.
type plan struct {
	WindowsSetup bool         `json:"windows_setup,omitempty"`
	LastJob      string       `json:"last_job,omitempty"`
	Link         string       `json:"link"`
	Folder       string       `json:"folder"` // the UGT folder, where results go
	Recipes      []recipeView `json:"recipes,omitempty"`
	Error        string       `json:"error,omitempty"`
}

type recipeView struct {
	Recipe            string           `json:"recipe"`
	What              string           `json:"what"`
	Kind              string           `json:"kind"`
	Repo              string           `json:"repo"`
	Inputs            []recipe.File    `json:"inputs,omitempty"`
	Outputs           []recipe.File    `json:"outputs,omitempty"`
	Provides          recipe.Provides  `json:"provides,omitzero"`
	InstallationLimit string           `json:"installation_limit,omitempty"`
	KnownIssues       []string         `json:"known_issues,omitempty"`
	Settings          []recipe.Setting `json:"settings,omitempty"`
	Access            []accessView     `json:"access,omitempty"`
	Verified          bool             `json:"verified"`
	Results           string           `json:"results"` // where its results go
}

type accessView struct {
	What string `json:"what"`
	Why  string `json:"why,omitempty"`
}

func (s *Server) plan(ctx context.Context) plan {
	s.planMu.Lock()
	defer s.planMu.Unlock()
	if len(s.shown) == 0 {
		s.planned = plan{Link: s.Link, Folder: s.App.Layout.Home, WindowsSetup: s.App.Runtime != nil && s.App.Runtime.Name() == "wsl"}
		l, err := link.Parse(s.Link)
		if err != nil {
			s.planned.Error = err.Error()
			return s.planned
		}
		entries, err := s.App.Linked(ctx, l)
		if err != nil {
			s.planned.Error = err.Error()
			return s.planned
		}
		s.shown = entries
		for _, e := range entries {
			v := recipeView{Recipe: e.Recipe, What: e.What, Kind: e.Kind, Repo: e.Repo, Inputs: e.Inputs, Outputs: e.Outputs,
				Provides: e.Provides, Settings: e.Settings, KnownIssues: e.KnownIssues, InstallationLimit: e.InstallationLimit, Verified: len(e.Verified) > 0}
			if id, err := recipe.ParseID(e.Recipe); err == nil {
				v.Results = s.App.Layout.Results(id)
			}
			for _, a := range e.Access {
				v.Access = append(v.Access, accessView{What: access.Heading(access.Kind(a.Kind)), Why: a.Why})
			}
			s.planned.Recipes = append(s.planned.Recipes, v)
		}
	}
	return s.planned
}

// entry is the plan's recipe id as the page showed it, so the page runs only
// what it showed, at the revision it showed.
func (s *Server) entry(ctx context.Context, id string) (catalogue.Entry, bool) {
	if p := s.plan(ctx); p.Error != "" {
		return catalogue.Entry{}, false
	}
	for _, e := range s.shown {
		if e.Recipe == id {
			return e, true
		}
	}
	return catalogue.Entry{}, false
}

// run starts the recipe the user chose, with the files they gave it. Files
// come as file:<input path>, texts as text:<input path> and value settings as
// value:<name>.
func (s *Server) run(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	parts, err := r.MultipartReader()
	if err != nil {
		replyError(w, http.StatusBadRequest, err)
		return
	}
	dir, err := os.MkdirTemp(s.uploads(), "run-")
	if err != nil {
		replyError(w, http.StatusInternalServerError, err)
		return
	}
	var id, instance string
	var inputs []string
	texts, values := map[string]string{}, map[string]string{}
	for {
		p, err := parts.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = os.RemoveAll(dir)
			replyError(w, http.StatusBadRequest, err)
			return
		}
		name := p.FormName()
		if strings.HasPrefix(name, "file:") && p.FileName() != "" {
			// Two inputs can both be called document.pdf. Keep both originals.
			sub, err := os.MkdirTemp(dir, "input-")
			if err == nil {
				dst := filepath.Join(sub, filepath.Base(p.FileName()))
				err = save(p, dst)
				if err == nil {
					inputs = append(inputs, strings.TrimPrefix(name, "file:")+"="+dst)
				}
			}
			if err != nil {
				_ = os.RemoveAll(dir)
				replyError(w, http.StatusInternalServerError, err)
				return
			}
			continue
		}
		field, err := small(p)
		if err != nil {
			_ = os.RemoveAll(dir)
			replyError(w, http.StatusBadRequest, err)
			return
		}
		switch {
		case name == "instance":
			instance = field
		case name == "recipe":
			id = field
		case strings.HasPrefix(name, "text:") && field != "":
			texts[strings.TrimPrefix(name, "text:")] = field
		case strings.HasPrefix(name, "value:") && field != "":
			values[strings.TrimPrefix(name, "value:")] = field
		}
	}
	e, ok := s.entry(ctx, id)
	if !ok {
		_ = os.RemoveAll(dir)
		replyError(w, http.StatusBadRequest, fmt.Errorf("%q is not a recipe this page offers", id))
		return
	}
	started, err := s.App.LinkedRun(ctx, e, instance, inputs, texts, values)
	if err != nil {
		_ = os.RemoveAll(dir)
		replyError(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	s.jobs[started] = true
	s.lastJob = started
	s.mu.Unlock()
	go func() {
		// the files given are the workspace's once the run has copied them in
		for {
			if j, err := s.App.Wait(started, time.Minute); err != nil || j.State.Done() {
				_ = os.RemoveAll(dir)
				return
			}
		}
	}()
	reply(w, map[string]string{"job": started})
}

func (s *Server) uploads() string {
	dir := filepath.Join(s.App.Layout.State, "uploads")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

// small reads a form field, which is never large.
func small(p io.Reader) (string, error) {
	const limit = 1 << 20
	b, err := io.ReadAll(io.LimitReader(p, limit+1))
	if len(b) > limit {
		return "", errors.New("this text is too long to paste; choose the file instead")
	}
	return string(b), err
}

func save(p io.Reader, path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, p)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// mine is a job this page started.
func (s *Server) mine(r *http.Request) (job.Job, bool) {
	id := r.URL.Query().Get("id")
	s.mu.Lock()
	_, ok := s.jobs[id]
	s.mu.Unlock()
	if !ok {
		return job.Job{}, false
	}
	j, err := s.App.Job(id)
	return j, err == nil
}

func (s *Server) job(w http.ResponseWriter, r *http.Request) {
	j, ok := s.mine(r)
	if !ok {
		replyError(w, http.StatusNotFound, errors.New("this page started no such run"))
		return
	}
	reply(w, j)
}

// setup uses the same setup operation as the installer and MCP. A returning
// user can finish setup after a restart without having to type a command.
func (s *Server) setup(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("recipe")
	if _, ok := s.entry(ctx, id); !ok || s.App.Runtime == nil || r.FormValue("agree") != "yes" {
		replyError(w, http.StatusBadRequest, errors.New("choose a project on this page and confirm setup first"))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastJob != "" {
		if last, err := s.App.Job(s.lastJob); err == nil && !last.State.Done() {
			replyError(w, http.StatusConflict, errors.New("a task is still running on this page; refresh to see its progress"))
			return
		}
	}
	j := s.App.Start(ctx, "setup", id, func(ctx context.Context, progress func(string, string)) (any, error) {
		return s.App.Setup(ctx, app.SetupOptions{AllowAdmin: s.App.Runtime.Name() == "wsl", Progress: progress})
	})
	s.jobs[j.ID] = true
	s.lastJob = j.ID
	reply(w, map[string]string{"job": j.ID})
}

// stop derives the installation from this page's checked result. A caller
// cannot supply a target or stop another page's service.
func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	j, mine := s.mine(r)
	res, ok := j.Result.(app.RunResult)
	if !mine || !ok || j.State != job.Succeeded || !res.Passed || res.Kind != "service" {
		replyError(w, http.StatusNotFound, errors.New("this run did not start an app"))
		return
	}
	target := res.Installation
	if target == "" {
		target = res.Recipe
	}
	if _, err := s.App.Stop(r.Context(), target); err != nil {
		replyError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reveal opens a finished run's results folder in the file manager.
func (s *Server) reveal(w http.ResponseWriter, r *http.Request) {
	j, ok := s.mine(r)
	res, isRun := j.Result.(app.RunResult)
	if !ok || !isRun || res.Results == "" || s.App.Browser == nil {
		replyError(w, http.StatusNotFound, errors.New("this run delivered no folder"))
		return
	}
	if err := s.App.Browser.Open(res.Results); err != nil {
		replyError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func replyError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
