package app

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/auscaster/ugt/internal/domain/ledger"
)

// fakeRuntime is a runtime whose file system is a temporary folder and whose
// engine runs Go functions instead of containers.
type fakeRuntime struct {
	root string
}

func (r *fakeRuntime) Name() string { return "fake" }
func (r *fakeRuntime) Status(context.Context) (RuntimeStatus, error) {
	return RuntimeStatus{Ready: true}, nil
}
func (r *fakeRuntime) Files() Files                               { return osFiles{} }
func (r *fakeRuntime) Exec(context.Context, Command) (int, error) { return 0, nil }
func (r *fakeRuntime) Workspaces() string                         { return r.root }

// fakeEngine runs each service as a Go function on the project directory.
type fakeEngine struct {
	config   ComposeConfig
	build    func(io.Writer) int
	services map[string]func(dir string, log io.Writer) int
	ran      []string
	downs    int
}

func (e *fakeEngine) Info(context.Context) (EngineInfo, error) {
	return EngineInfo{Version: "fake", Arch: "arm64"}, nil
}
func (e *fakeEngine) Config(context.Context, Project) (ComposeConfig, error) { return e.config, nil }
func (e *fakeEngine) Build(_ context.Context, _ Project, log io.Writer) (int, error) {
	e.ran = append(e.ran, "build")
	if e.build != nil {
		return e.build(log), nil
	}
	return 0, nil
}
func (e *fakeEngine) Run(_ context.Context, p Project, service string, log io.Writer) (int, error) {
	e.ran = append(e.ran, service)
	return e.services[service](p.Dir, log), nil
}
func (e *fakeEngine) Up(context.Context, Project, io.Writer) (int, error) { return 0, nil }
func (e *fakeEngine) Stop(context.Context, Project, io.Writer) error      { return nil }
func (e *fakeEngine) Down(context.Context, Project, bool, io.Writer) error {
	e.downs++
	return nil
}
func (e *fakeEngine) Pull(context.Context, string, io.Writer) error { return nil }
func (e *fakeEngine) Tag(context.Context, string, string) error     { return nil }

// osFiles implements Files on the host file system, for tests.
type osFiles struct{}

func (osFiles) MkdirAll(p string) error            { return os.MkdirAll(p, 0o755) }
func (osFiles) RemoveAll(p string) error           { return os.RemoveAll(p) }
func (osFiles) WriteFile(p string, b []byte) error { return os.WriteFile(p, b, 0o644) }
func (f osFiles) CopyIn(src, dst string) error     { return f.Copy(src, dst) }
func (f osFiles) CopyOut(src, dst string) error    { return f.Copy(src, dst) }
func (osFiles) Copy(src, dst string) error         { return os.CopyFS(dst, os.DirFS(src)) }
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

// osDisk implements Disk on the host file system, for tests.
type osDisk struct{}

func (osDisk) FS(dir string) fs.FS { return os.DirFS(dir) }
func (osDisk) WriteFile(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
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
