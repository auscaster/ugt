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

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/access"
	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/job"
	"github.com/auscaster/ugt/internal/domain/link"
	"github.com/auscaster/ugt/internal/domain/recipe"
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

	once    sync.Once
	planned plan

	mu   sync.Mutex
	last time.Time
	jobs map[string]string // the jobs this page started, and the folder of the files given for each
}

// Serve runs the page until it has been quiet for Idle with no run going, or
// ctx ends.
func (s *Server) Serve(ctx context.Context) error {
	if s.Idle == 0 {
		s.Idle = 2 * time.Minute
	}
	s.jobs = map[string]string{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	addr := ln.Addr().String()
	base := "/" + secret() + "/"
	files, _ := fs.Sub(static, "static")
	mux := http.NewServeMux()
	mux.Handle("GET "+base, http.StripPrefix(base, http.FileServerFS(files)))
	mux.HandleFunc("GET "+base+"api/plan", func(w http.ResponseWriter, r *http.Request) { reply(w, s.plan(ctx)) })
	mux.HandleFunc("GET "+base+"api/alive", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST "+base+"api/run", func(w http.ResponseWriter, r *http.Request) { s.run(ctx, w, r) })
	mux.HandleFunc("GET "+base+"api/job", s.job)
	mux.HandleFunc("POST "+base+"api/reveal", s.reveal)
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
	Link    string       `json:"link"`
	Folder  string       `json:"folder"` // the UGT folder, where results go
	Recipes []recipeView `json:"recipes,omitempty"`
	Error   string       `json:"error,omitempty"`
}

type recipeView struct {
	Recipe   string           `json:"recipe"`
	What     string           `json:"what"`
	Kind     string           `json:"kind"`
	Repo     string           `json:"repo"`
	Inputs   []recipe.File    `json:"inputs,omitempty"`
	Outputs  []recipe.File    `json:"outputs,omitempty"`
	Provides recipe.Provides  `json:"provides,omitzero"`
	Settings []recipe.Setting `json:"settings,omitempty"`
	Access   []accessView     `json:"access,omitempty"`
	Verified bool             `json:"verified"`
	Results  string           `json:"results"` // where its results go
}

type accessView struct {
	What string `json:"what"`
	Why  string `json:"why,omitempty"`
}

func (s *Server) plan(ctx context.Context) plan {
	s.once.Do(func() {
		s.planned = plan{Link: s.Link, Folder: s.App.Layout.Home}
		l, err := link.Parse(s.Link)
		if err != nil {
			s.planned.Error = err.Error()
			return
		}
		entries, err := s.App.Linked(ctx, l)
		if err != nil {
			s.planned.Error = err.Error()
			return
		}
		for _, e := range entries {
			v := recipeView{Recipe: e.Recipe, What: e.What, Kind: e.Kind, Repo: e.Repo, Inputs: e.Inputs, Outputs: e.Outputs,
				Provides: e.Provides, Settings: e.Settings, Verified: len(e.Verified) > 0}
			if id, err := recipe.ParseID(e.Recipe); err == nil {
				v.Results = s.App.Layout.Results(id)
			}
			for _, a := range e.Access {
				v.Access = append(v.Access, accessView{What: access.Heading(access.Kind(a.Kind)), Why: a.Why})
			}
			s.planned.Recipes = append(s.planned.Recipes, v)
		}
	})
	return s.planned
}

// entry is the plan's recipe id, so the page runs only what it showed.
func (s *Server) entry(ctx context.Context, id string) (catalogue.Entry, bool) {
	l, err := link.Parse(s.Link)
	if err != nil {
		return catalogue.Entry{}, false
	}
	entries, err := s.App.Linked(ctx, l)
	if err != nil {
		return catalogue.Entry{}, false
	}
	for _, e := range entries {
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
	var id string
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
		switch {
		case name == "recipe":
			id = small(p)
		case strings.HasPrefix(name, "file:") && p.FileName() != "":
			saved := filepath.Join(dir, filepath.Base(p.FileName()))
			if err := save(p, saved); err != nil {
				_ = os.RemoveAll(dir)
				replyError(w, http.StatusInternalServerError, err)
				return
			}
			inputs = append(inputs, strings.TrimPrefix(name, "file:")+"="+saved)
		case strings.HasPrefix(name, "text:"):
			if t := small(p); t != "" {
				texts[strings.TrimPrefix(name, "text:")] = t
			}
		case strings.HasPrefix(name, "value:"):
			if v := small(p); v != "" {
				values[strings.TrimPrefix(name, "value:")] = v
			}
		}
	}
	e, ok := s.entry(ctx, id)
	if !ok {
		_ = os.RemoveAll(dir)
		replyError(w, http.StatusBadRequest, fmt.Errorf("%q is not a recipe this page offers", id))
		return
	}
	started, err := s.App.LinkedRun(ctx, e, inputs, texts, values)
	if err != nil {
		_ = os.RemoveAll(dir)
		replyError(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	s.jobs[started] = dir
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
func small(p io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(p, 1<<20))
	return string(b)
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
