// Package wsl implements the Runtime port on Windows: UGT's own WSL distro,
// running Docker Engine from UGT's Linux layer. Programs run in it through
// wsl.exe; files cross into it through \\wsl.localhost. Setup installs WSL
// when the user allows it, imports the published layer as the distro, and
// updates the layer in place when this ugt runs a newer one.
package wsl

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/auscaster/ugt/internal/app"
)

// Config is the layer contract and where the distro's disk lives.
type Config struct {
	Distro       string // UGT
	Workspaces   string // where workspaces live in the distro
	Engine       string // the command that starts the engine and waits for it
	Layer        string // the published layer this ugt runs on, by revision
	Revision     string // that layer's revision
	RevisionFile string // where the distro records the revision of its layer
	Update       string // the script that updates the layer in place from its tar on stdin
	InstallDir   string // where the distro's disk and the downloaded layer go
}

// Fetch downloads a layer's file system for an architecture into dir, as a
// gzipped tar, and returns its path.
type Fetch func(ctx context.Context, image, arch, dir string, progress func(string)) (string, error)

// Runtime is UGT's WSL distro.
type Runtime struct {
	cfg   Config
	host  app.Files // the host file system, which reaches the distro through \\wsl.localhost
	run   runner
	fetch Fetch

	uncOnce sync.Once
	uncRoot string
}

// runner runs a Windows program; tests replace it.
type runner func(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error)

var _ app.Runtime = (*Runtime)(nil)

// New returns the WSL runtime. host is the Files port on the Windows file
// system, used for the distro's files through their \\wsl.localhost paths;
// fetch downloads the layer.
func New(cfg Config, host app.Files, fetch Fetch) *Runtime {
	return &Runtime{cfg: cfg, host: host, run: execRun, fetch: fetch}
}

func (r *Runtime) Name() string       { return "wsl" }
func (r *Runtime) Workspaces() string { return r.cfg.Workspaces }
func (r *Runtime) Files() app.Files   { return &files{host: r.host, root: r.unc, chmod: r.chmod} }

// chmod sets a mode in the distro: Windows cannot set Linux modes through
// \\wsl.localhost.
func (r *Runtime) chmod(p string) error {
	var out bytes.Buffer
	if exit, err := r.inDistro(context.Background(), "", nil, []string{"chmod", "0777", p}, nil, &out, &out); err != nil || exit != 0 {
		return fmt.Errorf("chmod %s in the distro (exit %d): %v %s", p, exit, err, strings.TrimSpace(out.String()))
	}
	return nil
}

// Status reports whether WSL, the distro and its engine are ready, starting
// the engine if the distro is up but the engine is not. A distro on another
// revision of the layer than this ugt's is not ready: setup updates it.
func (r *Runtime) Status(ctx context.Context) (app.RuntimeStatus, error) {
	if !r.wslInstalled(ctx) {
		return app.RuntimeStatus{Problem: "WSL, which UGT runs its engine in, is not installed.", Fix: "Run ugt setup --yes: it installs WSL, which asks for admin approval and may need a restart."}, nil
	}
	if ok, err := r.distroExists(ctx); err != nil {
		return app.RuntimeStatus{}, err
	} else if !ok {
		return app.RuntimeStatus{Problem: "UGT's engine is not set up yet.", Fix: "Run ugt setup."}, nil
	}
	if have := r.revision(ctx); have != r.cfg.Revision {
		return app.RuntimeStatus{Problem: fmt.Sprintf("UGT's engine runs layer %q, and this UGT runs layer %s.", have, r.cfg.Revision),
			Fix: "Run ugt setup: it updates the layer and keeps every image and saved data."}, nil
	}
	var out bytes.Buffer
	if exit, err := r.inDistro(ctx, "", nil, []string{r.cfg.Engine}, nil, &out, &out); err != nil || exit != 0 {
		return app.RuntimeStatus{
			Problem: "UGT's engine did not start: " + lastLines(out.String(), 5),
			Fix:     "Run ugt doctor, then ugt setup to repair it.",
		}, nil
	}
	return app.RuntimeStatus{Ready: true}, nil
}

// revision is the revision of the layer in the distro, or "" when it records
// none.
func (r *Runtime) revision(ctx context.Context) string {
	var out bytes.Buffer
	if exit, err := r.inDistro(ctx, "", nil, []string{"cat", r.cfg.RevisionFile}, nil, &out, io.Discard); err != nil || exit != 0 {
		return ""
	}
	return strings.TrimSpace(out.String())
}

// Folder mounts a Windows folder into the distro for one command, at a path
// of its own, and unmounts it on release: the distro mounts no Windows drive
// otherwise, so a command reaches exactly the folder it was run in.
func (r *Runtime) Folder(ctx context.Context, dir string) (string, func(), error) {
	var id [6]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", nil, err
	}
	at := "/mnt/ugt/" + hex.EncodeToString(id[:])
	var out strings.Builder
	for _, argv := range [][]string{{"mkdir", "-p", at}, {"mount", "-t", "drvfs", dir, at}} {
		if exit, err := r.inDistro(ctx, "", nil, argv, nil, &out, &out); err != nil || exit != 0 {
			_, _ = r.inDistro(context.WithoutCancel(ctx), "", nil, []string{"rmdir", at}, nil, nil, nil)
			return "", nil, fmt.Errorf("mount %s into UGT's engine (exit %d): %v %s", dir, exit, err, strings.TrimSpace(out.String()))
		}
	}
	release := func() {
		ctx := context.WithoutCancel(ctx)
		_, _ = r.inDistro(ctx, "", nil, []string{"umount", at}, nil, nil, nil)
		_, _ = r.inDistro(ctx, "", nil, []string{"rmdir", at}, nil, nil, nil)
	}
	return at, release, nil
}

// Exec runs a program in the distro, as root, in c.Dir.
func (r *Runtime) Exec(ctx context.Context, c app.Command) (int, error) {
	return r.inDistro(ctx, c.Dir, c.Env, append([]string{c.Name}, c.Args...), c.Stdin, c.Stdout, c.Stderr)
}

// inDistro runs argv in the distro with wsl.exe --exec, so no shell parses
// it. The environment goes in through env(1), sorted.
func (r *Runtime) inDistro(ctx context.Context, dir string, env map[string]string, argv []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	args := []string{"--distribution", r.cfg.Distro, "--user", "root"}
	if dir != "" {
		args = append(args, "--cd", dir)
	}
	args = append(args, "--exec")
	if len(env) > 0 {
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		args = append(args, "env")
		for _, k := range keys {
			args = append(args, k+"="+env[k])
		}
	}
	return r.run(ctx, "wsl.exe", append(args, argv...), stdin, stdout, stderr)
}

// Setup installs WSL when allowed, imports the layer as the distro or updates
// the distro's layer in place, and starts the engine. Each step is skipped
// when it is already done, so running setup again repairs what is missing.
func (r *Runtime) Setup(ctx context.Context, opts app.SetupOptions) (app.SetupResult, error) {
	progress := opts.Progress
	if progress == nil {
		progress = func(string, string) {}
	}
	res := app.SetupResult{Runtime: r.Name()}
	if !r.wslInstalled(ctx) {
		if !opts.AllowAdmin {
			res.Next = []string{"WSL needs installing first. That asks for admin approval and may restart Windows: run ugt setup --yes."}
			return res, nil
		}
		progress("wsl", "installing WSL; Windows will ask for admin approval")
		var out bytes.Buffer
		if exit, err := r.run(ctx, "wsl.exe", []string{"--install", "--no-distribution"}, nil, &out, &out); err != nil || exit != 0 {
			return res, fmt.Errorf("wsl --install failed (exit %d): %v %s", exit, err, lastLines(decode(out.Bytes()), 5))
		}
		res.Done = append(res.Done, "installed WSL")
		res.NeedsRestart = true
		res.Next = []string{"Restart Windows, then run ugt setup again to finish."}
		return res, nil
	}

	exists, err := r.distroExists(ctx)
	if err != nil {
		return res, err
	}
	have := ""
	if exists {
		have = r.revision(ctx)
	}
	if !exists || have != r.cfg.Revision {
		layers := filepath.Join(r.cfg.InstallDir, "layers")
		tarball, err := r.fetch(ctx, r.cfg.Layer, runtime.GOARCH, layers, func(m string) { progress("download", m) })
		if err != nil {
			return res, err
		}
		if !exists {
			if err := r.importLayer(ctx, tarball, progress); err != nil {
				return res, err
			}
			res.Done = append(res.Done, "created the WSL distro "+r.cfg.Distro+" from UGT's Linux layer "+r.cfg.Revision)
		} else {
			if err := r.updateLayer(ctx, tarball, progress); err != nil {
				return res, err
			}
			res.Done = append(res.Done, fmt.Sprintf("updated UGT's Linux layer from %q to %s, keeping every image and saved data", have, r.cfg.Revision))
		}
		forgetOthers(layers, tarball)
	}
	res.Created = "wsl:" + r.cfg.Distro
	// the layer's wsl.conf (boot command, no Windows drives) applies from the next start
	if _, err := r.run(ctx, "wsl.exe", []string{"--terminate", r.cfg.Distro}, nil, io.Discard, io.Discard); err != nil {
		return res, err
	}
	progress("engine", "starting the engine")
	status, err := r.Status(ctx)
	if err != nil {
		return res, err
	}
	if !status.Ready {
		res.Next = []string{status.Problem + " " + status.Fix}
		return res, nil
	}
	res.Ready = true
	res.Done = append(res.Done, "started the engine")
	return res, nil
}

// Teardown unregisters the distro, which deletes its disk with the engine,
// images and saved data in it, and removes the downloaded root file system.
func (r *Runtime) Teardown(ctx context.Context) error {
	if exists, err := r.distroExists(ctx); err != nil {
		return err
	} else if exists {
		var out bytes.Buffer
		if exit, err := r.run(ctx, "wsl.exe", []string{"--unregister", r.cfg.Distro}, nil, &out, &out); err != nil || exit != 0 {
			return fmt.Errorf("wsl --unregister %s failed (exit %d): %v %s", r.cfg.Distro, exit, err, lastLines(decode(out.Bytes()), 5))
		}
	}
	return os.RemoveAll(r.cfg.InstallDir)
}

// importLayer creates the distro from the layer.
func (r *Runtime) importLayer(ctx context.Context, tarball string, progress func(string, string)) error {
	disk := filepath.Join(r.cfg.InstallDir, "disk")
	if err := os.MkdirAll(disk, 0o755); err != nil {
		return err
	}
	progress("import", "creating the WSL distro "+r.cfg.Distro)
	var out bytes.Buffer
	if exit, err := r.run(ctx, "wsl.exe", []string{"--import", r.cfg.Distro, disk, tarball, "--version", "2"}, nil, &out, &out); err != nil || exit != 0 {
		return fmt.Errorf("wsl --import %s failed (exit %d): %v %s\nIf WSL says it needs an update, run wsl --update, then ugt setup again", r.cfg.Distro, exit, err, lastLines(decode(out.Bytes()), 5))
	}
	return nil
}

// updateLayer replaces the distro's layer with this one in place: its
// images, saved data and workspaces are not the layer's, and stay.
func (r *Runtime) updateLayer(ctx context.Context, tarball string, progress func(string, string)) error {
	f, err := os.Open(tarball)
	if err != nil {
		return err
	}
	defer f.Close()
	progress("update", "updating UGT's Linux layer to "+r.cfg.Revision)
	var out bytes.Buffer
	if exit, err := r.inDistro(ctx, "", nil, []string{"sh", "-c", r.cfg.Update}, f, &out, &out); err != nil || exit != 0 {
		return fmt.Errorf("updating UGT's Linux layer failed (exit %d): %v\n%s", exit, err, lastLines(out.String(), 15))
	}
	return nil
}

// forgetOthers removes the layers downloaded before this one.
func forgetOthers(dir, keep string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if p := filepath.Join(dir, e.Name()); p != keep {
			_ = os.RemoveAll(p)
		}
	}
}

// wslInstalled reports whether WSL answers: --version for the Store build,
// --status for the one that ships with Windows. Either may fail where the
// other answers, for example --status with no distro installed yet.
func (r *Runtime) wslInstalled(ctx context.Context) bool {
	for _, arg := range []string{"--version", "--status"} {
		if exit, err := r.run(ctx, "wsl.exe", []string{arg}, nil, io.Discard, io.Discard); err == nil && exit == 0 {
			return true
		}
	}
	return false
}

// distroExists reports whether the distro is registered.
func (r *Runtime) distroExists(ctx context.Context) (bool, error) {
	var out bytes.Buffer
	exit, err := r.run(ctx, "wsl.exe", []string{"--list", "--quiet"}, nil, &out, io.Discard)
	if err != nil {
		return false, err
	}
	if exit != 0 {
		return false, nil // no distros at all also exits non-zero
	}
	for _, line := range strings.Split(decode(out.Bytes()), "\n") {
		if strings.EqualFold(strings.TrimSpace(line), r.cfg.Distro) {
			return true, nil
		}
	}
	return false, nil
}

// unc returns the Windows path of a distro path: \\wsl.localhost\UGT\..., or
// \\wsl$\UGT\... on Windows 10 builds without wsl.localhost.
func (r *Runtime) unc(p string) string {
	r.uncOnce.Do(func() {
		r.uncRoot = `\\wsl.localhost\` + r.cfg.Distro + `\`
		if _, err := os.Stat(r.uncRoot); err != nil {
			if _, err := os.Stat(`\\wsl$\` + r.cfg.Distro + `\`); err == nil {
				r.uncRoot = `\\wsl$\` + r.cfg.Distro + `\`
			}
		}
	})
	return r.uncRoot + strings.ReplaceAll(strings.TrimPrefix(p, "/"), "/", `\`)
}

// decode reads wsl.exe's output, which older versions write in UTF-16 even
// when asked for UTF-8.
func decode(b []byte) string {
	if len(b) >= 2 && (bytes.IndexByte(b, 0) >= 0) {
		if b[0] == 0xFF && b[1] == 0xFE {
			b = b[2:]
		}
		u := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
		}
		return strings.ReplaceAll(string(utf16.Decode(u)), "\r", "")
	}
	return strings.ReplaceAll(string(b), "\r", "")
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// execRun runs a Windows program with no console window of its own, so an AI
// app's MCP server never flashes windows on screen.
func execRun(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, orDiscard(stdout), orDiscard(stderr)
	hideWindow(cmd)
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return -1, fmt.Errorf("run %s: %w", name, err)
	}
	return 0, nil
}

func orDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

// files is the distro's file system, reached through its Windows paths.
type files struct {
	host  app.Files
	root  func(string) string
	chmod func(string) error
}

func (f *files) Share(p string) error { return f.chmod(p) }

func (f *files) MkdirAll(p string) error               { return f.host.MkdirAll(f.root(p)) }
func (f *files) RemoveAll(p string) error              { return f.host.RemoveAll(f.root(p)) }
func (f *files) CopyIn(hostPath, p string) error       { return f.host.Copy(hostPath, f.root(p)) }
func (f *files) CopyOut(p, hostPath string) error      { return f.host.Copy(f.root(p), hostPath) }
func (f *files) Copy(source, target string) error      { return f.host.Copy(f.root(source), f.root(target)) }
func (f *files) ReadFile(p string) ([]byte, error)     { return f.host.ReadFile(f.root(p)) }
func (f *files) WriteFile(p string, data []byte) error { return f.host.WriteFile(f.root(p), data) }

// List returns the distro paths of every file under dir.
func (f *files) List(dir string) ([]app.FileInfo, error) {
	root := f.root(dir)
	list, err := f.host.List(root)
	if err != nil {
		return nil, err
	}
	prefix := strings.ReplaceAll(root, `\`, "/")
	for i, fi := range list {
		rel := strings.TrimPrefix(strings.ReplaceAll(fi.Path, `\`, "/"), prefix)
		list[i].Path = path.Join(dir, rel)
	}
	return list, nil
}
