package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/compose"
	"github.com/auscaster/ugt/internal/domain/ledger"
)

// fakeRuntime is a runtime whose file system is a temporary folder and whose
// engine runs Go functions instead of containers.
type fakeRuntime struct {
	root     string
	tornDown bool
}

func (r *fakeRuntime) Name() string { return "fake" }
func (r *fakeRuntime) Status(context.Context) (RuntimeStatus, error) {
	return RuntimeStatus{Ready: true}, nil
}
func (r *fakeRuntime) Files() Files                               { return osFiles{} }
func (r *fakeRuntime) Exec(context.Context, Command) (int, error) { return 0, nil }
func (r *fakeRuntime) Workspaces() string                         { return r.root }
func (r *fakeRuntime) Teardown(context.Context) error             { r.tornDown = true; return nil }
func (r *fakeRuntime) Setup(context.Context, SetupOptions) (SetupResult, error) {
	return SetupResult{Runtime: "fake", Ready: true}, nil
}

// fakeEngine runs each service as a Go function on the project directory.
type fakeEngine struct {
	config    compose.Model
	build     func(io.Writer) int
	services  map[string]func(dir string, log io.Writer) int
	ran       []string
	commands  []Project // the projects commands ran in
	up        func(io.Writer) int
	logs      string          // what the containers logged
	images    map[string]bool // what the engine holds
	running   bool
	lastEnv   map[string]string   // the environment the stack last started with
	memory    int64               // the engine's memory; 0 when unknown
	volumesOf map[string][]string // named volumes by project
	downs     int
	volumes   int // downs that removed volumes
}

func (e *fakeEngine) Info(context.Context) (EngineInfo, error) {
	return EngineInfo{Version: "fake", Arch: "arm64", MemoryBytes: e.memory}, nil
}

// Config resolves built services' images to $UGT_IMAGE, as Compose does.
func (e *fakeEngine) Config(_ context.Context, p Project) (compose.Model, error) {
	cfg := compose.Model{Services: map[string]compose.Service{}}
	for name, svc := range e.config.Services {
		if svc.Builds() && svc.Image == "" {
			svc.Image = p.Env["UGT_IMAGE"]
		}
		cfg.Services[name] = svc
	}
	return cfg, nil
}
func (e *fakeEngine) Build(_ context.Context, p Project, log io.Writer) (int, error) {
	e.ran = append(e.ran, "build")
	if e.images == nil {
		e.images = map[string]bool{}
	}
	for _, svc := range e.config.Services {
		if svc.Builds() {
			e.images[p.Env["UGT_IMAGE"]] = true
		}
	}
	if e.build != nil {
		return e.build(log), nil
	}
	return 0, nil
}
func (e *fakeEngine) Run(_ context.Context, p Project, service string, command []string, log io.Writer) (int, error) {
	if len(command) > 0 {
		e.ran = append(e.ran, service+": "+strings.Join(command, " "))
		e.commands = append(e.commands, p)
		return 0, nil
	}
	e.ran = append(e.ran, service)
	return e.services[service](p.Dir, log), nil
}
func (e *fakeEngine) Up(_ context.Context, p Project, log io.Writer) (int, error) {
	e.ran = append(e.ran, "up")
	e.lastEnv = p.Env
	if e.up != nil {
		return e.up(log), nil
	}
	return 0, nil
}
func (e *fakeEngine) Logs(_ context.Context, _ Project, log io.Writer) error {
	_, err := io.WriteString(log, e.logs)
	return err
}
func (e *fakeEngine) Stop(context.Context, Project, io.Writer) error { return nil }
func (e *fakeEngine) Down(_ context.Context, _ Project, volumes bool, _ io.Writer) error {
	e.downs++
	if volumes {
		e.volumes++
	}
	return nil
}
func (e *fakeEngine) Pull(_ context.Context, ref string, _ io.Writer) error {
	e.ran = append(e.ran, "pull "+ref)
	return nil
}
func (e *fakeEngine) Tag(_ context.Context, source, target string) error {
	if e.images == nil {
		e.images = map[string]bool{}
	}
	e.images[target] = true
	return nil
}
func (e *fakeEngine) Port(_ context.Context, _ Project, service string, port int) (string, error) {
	return fmt.Sprintf("127.0.0.1:%d", 50000+port), nil
}
func (e *fakeEngine) Running(context.Context, Project) (bool, error) { return e.running, nil }
func (e *fakeEngine) ImageExists(_ context.Context, ref string) (bool, error) {
	return e.images[ref], nil
}
func (e *fakeEngine) RemoveImage(_ context.Context, ref string) error {
	e.ran = append(e.ran, "rmi "+ref)
	delete(e.images, ref)
	return nil
}
func (e *fakeEngine) Volumes(_ context.Context, project string) ([]string, error) {
	return e.volumesOf[project], nil
}
func (e *fakeEngine) RemoveVolume(_ context.Context, name string) error {
	e.ran = append(e.ran, "rmvol "+name)
	return nil
}
func (e *fakeEngine) ImageDigest(_ context.Context, ref string) (string, error) {
	if strings.Contains(ref, "missing") {
		return "", fmt.Errorf("%s: not found", ref)
	}
	sum := sha256.Sum256([]byte(ref))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// fakeGitHub serves repositories from memory: files by "owner/name/path".
type fakeGitHub struct {
	repos    map[string]Repo
	files    map[string]string
	proposed []Proposal
	approve  bool // whether the user approves a device login
}

func (g *fakeGitHub) StartLogin(context.Context) (DeviceLogin, error) {
	return DeviceLogin{Code: "WDJB-MJHT", URL: "https://github.com/login/device", Device: "dev"}, nil
}
func (g *fakeGitHub) FinishLogin(context.Context, DeviceLogin) (string, string, error) {
	if !g.approve {
		return "", "", errors.New("the sign-in was declined on GitHub")
	}
	return "gho_user", "someone", nil
}

func (g *fakeGitHub) Propose(_ context.Context, p Proposal) (string, error) {
	g.proposed = append(g.proposed, p)
	return "https://github.com/" + p.Repo + "/pull/1", nil
}

const fakeCommit = "86098128c0b4f24f0e2aa2994de830614b474227"

func (g *fakeGitHub) Repo(_ context.Context, owner, name string) (Repo, error) {
	if r, ok := g.repos[owner+"/"+name]; ok {
		return r, nil
	}
	return Repo{}, fmt.Errorf("github.com/%s/%s: %w", owner, name, ErrNotFound)
}
func (g *fakeGitHub) Commit(context.Context, string, string, string) (string, error) {
	return fakeCommit, nil
}
func (g *fakeGitHub) List(_ context.Context, owner, name, _, dir string) ([]Entry, error) {
	prefix := owner + "/" + name + "/"
	if dir != "" {
		prefix += dir + "/"
	}
	seen := map[string]bool{}
	var out []Entry
	for p := range g.files {
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok {
			continue
		}
		first, _, more := strings.Cut(rest, "/")
		if !seen[first] {
			seen[first] = true
			out = append(out, Entry{Path: strings.TrimPrefix(dir+"/"+first, "/"), Dir: more})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: %w", dir, ErrNotFound)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
func (g *fakeGitHub) File(_ context.Context, owner, name, _, p string) ([]byte, error) {
	if data, ok := g.files[owner+"/"+name+"/"+p]; ok {
		return []byte(data), nil
	}
	return nil, fmt.Errorf("%s: %w", p, ErrNotFound)
}

// osFiles implements Files on the host file system, for tests.
type osFiles struct{}

func (osFiles) MkdirAll(p string) error            { return os.MkdirAll(p, 0o755) }
func (osFiles) RemoveAll(p string) error           { return os.RemoveAll(p) }
func (osFiles) WriteFile(p string, b []byte) error { return os.WriteFile(p, b, 0o644) }
func (osFiles) ReadFile(p string) ([]byte, error)  { return os.ReadFile(p) }
func (osFiles) Share(p string) error               { return os.Chmod(p, 0o777) }
func (f osFiles) CopyIn(src, dst string) error     { return f.Copy(src, dst) }
func (f osFiles) CopyOut(src, dst string) error    { return f.Copy(src, dst) }
func (osFiles) Copy(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	}
	// like the host adapter: merge into dst, overwriting what is there
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
func (osFiles) List(dir string) ([]FileInfo, error) {
	var out []FileInfo
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, FileInfo{Path: filepath.ToSlash(p), Size: info.Size()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

// fakeCatalogue serves recipe folders on disk, by ID.
type fakeCatalogue struct {
	source catalogue.Source
	dirs   map[string]string
	images map[string]catalogue.Image // what it publishes for every recipe, by suffix
}

func (c *fakeCatalogue) entry(dir, id string) catalogue.Entry {
	e := catalogue.FromFolder(os.DirFS(dir), id, c.source)
	e.Images = c.images
	return e
}

func (c *fakeCatalogue) Source() catalogue.Source { return c.source }
func (c *fakeCatalogue) Project(_ context.Context, owner, repo string) ([]catalogue.Entry, error) {
	var out []catalogue.Entry
	for id, dir := range c.dirs {
		if strings.HasPrefix(id, strings.ToLower(owner+"/"+repo+"/")) {
			out = append(out, c.entry(dir, id))
		}
	}
	return out, nil
}
func (c *fakeCatalogue) Search(_ context.Context, words []string) ([]catalogue.Entry, error) {
	var out []catalogue.Entry
	for id, dir := range c.dirs {
		match := len(words) > 0
		for _, w := range words {
			match = match && strings.Contains(id, w)
		}
		if match {
			out = append(out, c.entry(dir, id))
		}
	}
	return out, nil
}
func (c *fakeCatalogue) Open(_ context.Context, id string) (fs.FS, error) {
	if dir, ok := c.dirs[id]; ok {
		return os.DirFS(dir), nil
	}
	return nil, fmt.Errorf("%s: %w", id, ErrNotFound)
}

// osDisk implements Disk on the host file system, for tests.
type osDisk struct{}

func (osDisk) FS(dir string) fs.FS { return os.DirFS(dir) }
func (osDisk) WriteFile(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
func (osDisk) RemoveAll(p string) error { return os.RemoveAll(p) }
func (osDisk) Exists(p string) (bool, error) {
	_, err := os.Lstat(p)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

type memLedger struct{ entries []ledger.Entry }

func (l *memLedger) Record(e ledger.Entry) error      { l.entries = append(l.entries, e); return nil }
func (l *memLedger) Entries() ([]ledger.Entry, error) { return l.entries, nil }

// fixedClock advances one second per call, so durations are positive.
func fixedClock() Clock {
	t := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	return func() time.Time { t = t.Add(time.Second); return t }
}
