// Package host is this machine's own operating system. It implements the
// Runtime port for an engine reached from the host itself (the native engine on
// Linux, or one the user already runs, such as Docker Desktop, OrbStack or
// Colima, through the current docker context) and the Disk port for the
// user's own folders, whatever the runtime.
package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/auscaster/ugt/internal/app"
)

// Runtime is the host runtime.
type Runtime struct {
	workspaces string
	docker     string // the docker CLI's path, found on PATH
}

// New returns the host runtime with recipe workspaces under workspaces.
func New(workspaces string) *Runtime {
	return &Runtime{workspaces: filepath.ToSlash(workspaces), docker: "docker"}
}

var _ app.Runtime = (*Runtime)(nil)

func (r *Runtime) Name() string       { return "host" }
func (r *Runtime) Workspaces() string { return r.workspaces }
func (r *Runtime) Files() app.Files   { return Files{} }

// Status reports whether a docker engine answers from this host.
func (r *Runtime) Status(ctx context.Context) (app.RuntimeStatus, error) {
	if _, err := exec.LookPath(r.docker); err != nil {
		return app.RuntimeStatus{
			Problem: "No container engine is installed.",
			Fix:     "Run ugt setup, or install Docker Engine (Linux) or start Docker Desktop, OrbStack or Colima (macOS).",
		}, nil
	}
	cmd := exec.CommandContext(ctx, r.docker, "info", "--format", "{{.ServerVersion}}")
	if out, err := cmd.CombinedOutput(); err != nil {
		return app.RuntimeStatus{
			Problem: "The container engine is not running: " + firstLine(string(out)),
			Fix:     "Start Docker Desktop, OrbStack or Colima (macOS), or the docker service (Linux), then try again.",
		}, nil
	}
	return app.RuntimeStatus{Ready: true}, nil
}

// Setup uses the engine already on this machine. It installs nothing: on
// macOS the engine is Docker Desktop, OrbStack or Colima until UGT's own VM
// exists, and on Linux it is the distribution's Docker Engine.
func (r *Runtime) Setup(ctx context.Context, opts app.SetupOptions) (app.SetupResult, error) {
	res := app.SetupResult{Runtime: r.Name()}
	status, err := r.Status(ctx)
	if err != nil {
		return res, err
	}
	if status.Ready {
		res.Ready = true
		res.Done = []string{"uses the container engine already on this machine"}
		return res, nil
	}
	res.Next = []string{status.Problem + " " + status.Fix}
	switch runtime.GOOS {
	case "darwin":
		res.Next = append(res.Next, "Docker Desktop: https://www.docker.com/products/docker-desktop/ ; OrbStack: https://orbstack.dev ; Colima: brew install colima docker docker-compose docker-buildx && colima start")
	case "linux":
		res.Next = append(res.Next, "Docker Engine: https://docs.docker.com/engine/install/ , then add yourself to the docker group")
	}
	return res, nil
}

// Exec runs a program on the host.
func (r *Runtime) Exec(ctx context.Context, c app.Command) (int, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = os.Environ()
	for k, v := range c.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdout, cmd.Stderr = orDiscard(c.Stdout), orDiscard(c.Stderr)
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return -1, fmt.Errorf("run %s: %w", c.Name, err)
	}
	return 0, nil
}

func orDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' {
			return s[:i]
		}
	}
	return s
}

// Files implements the Files port on the host file system.
type Files struct{}

var _ app.Files = Files{}

func (Files) MkdirAll(path string) error               { return os.MkdirAll(path, 0o755) }
func (Files) RemoveAll(path string) error              { return os.RemoveAll(path) }
func (Files) WriteFile(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }
func (Files) ReadFile(path string) ([]byte, error)     { return os.ReadFile(path) }

// Share makes a folder writable by every user, so a container running as any
// user can write to it through a bind mount. On Windows it only clears the
// read-only attribute; containers there run in a runtime with its own files.
func (Files) Share(path string) error               { return os.Chmod(path, 0o777) }
func (f Files) CopyIn(hostPath, path string) error  { return f.Copy(hostPath, path) }
func (f Files) CopyOut(path, hostPath string) error { return f.Copy(path, hostPath) }

// Copy copies a file, or a folder and everything in it, to target.
func (Files) Copy(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(source, target, info.Mode())
	}
	return filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, dst, info.Mode())
	})
}

func copyFile(source, target string, mode fs.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// List returns every regular file under dir with its size, in path order.
func (Files) List(dir string) ([]app.FileInfo, error) {
	var out []app.FileInfo
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, app.FileInfo{Path: filepath.ToSlash(path), Size: info.Size()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

// Disk implements the Disk port on the host file system.
type Disk struct{}

var _ app.Disk = Disk{}

func (Disk) FS(dir string) fs.FS { return os.DirFS(dir) }

func (Disk) WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (Disk) Exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
