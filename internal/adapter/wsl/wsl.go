// Package wsl implements the Runtime port on Windows: UGT's own WSL distro,
// running Docker Engine from UGT's Linux layer. Programs run in it through
// wsl.exe; files cross into it through \\wsl.localhost. Setup installs WSL
// when the user allows it, imports Alpine Linux as the distro and provisions
// the layer in it.
package wsl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"gopkg.in/yaml.v3"

	"github.com/auscaster/ugt/internal/app"
)

// Config is the layer contract and where the distro's disk lives.
type Config struct {
	Distro       string // UGT
	Workspaces   string // where workspaces live in the distro
	Engine       string // the command that starts the engine and waits for it
	Provision    string // the script that provisions the layer
	AlpineBranch string // v3.24
	InstallDir   string // where the distro's disk and the downloaded root file system go
}

// Runtime is UGT's WSL distro.
type Runtime struct {
	cfg    Config
	host   app.Files // the host file system, which reaches the distro through \\wsl.localhost
	run    runner
	http   *http.Client
	alpine string // Alpine's mirror, https://dl-cdn.alpinelinux.org/alpine

	uncOnce sync.Once
	uncRoot string
}

// runner runs a Windows program; tests replace it.
type runner func(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error)

var _ app.Runtime = (*Runtime)(nil)

// New returns the WSL runtime. host is the Files port on the Windows file
// system, used for the distro's files through their \\wsl.localhost paths.
func New(cfg Config, host app.Files) *Runtime {
	return &Runtime{cfg: cfg, host: host, run: execRun, http: &http.Client{Timeout: 10 * time.Minute},
		alpine: "https://dl-cdn.alpinelinux.org/alpine"}
}

func (r *Runtime) Name() string       { return "wsl" }
func (r *Runtime) Workspaces() string { return r.cfg.Workspaces }
func (r *Runtime) Files() app.Files   { return &files{host: r.host, root: r.unc} }

// Status reports whether WSL, the distro and its engine are ready, starting
// the engine if the distro is up but the engine is not.
func (r *Runtime) Status(ctx context.Context) (app.RuntimeStatus, error) {
	if !r.wslInstalled(ctx) {
		return app.RuntimeStatus{Problem: "WSL, which UGT runs its engine in, is not installed.", Fix: "Run ugt setup --yes: it installs WSL, which asks for admin approval and may need a restart."}, nil
	}
	if ok, err := r.distroExists(ctx); err != nil {
		return app.RuntimeStatus{}, err
	} else if !ok {
		return app.RuntimeStatus{Problem: "UGT's engine is not set up yet.", Fix: "Run ugt setup."}, nil
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

// Exec runs a program in the distro, as root, in c.Dir.
func (r *Runtime) Exec(ctx context.Context, c app.Command) (int, error) {
	return r.inDistro(ctx, c.Dir, c.Env, append([]string{c.Name}, c.Args...), nil, c.Stdout, c.Stderr)
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

// Setup installs WSL when allowed, imports Alpine Linux as the distro,
// provisions the layer and starts the engine. Each step is skipped when it is
// already done, so running setup again repairs what is missing.
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
	if !exists {
		tarball, err := r.rootfs(ctx, progress)
		if err != nil {
			return res, err
		}
		disk := filepath.Join(r.cfg.InstallDir, "disk")
		if err := os.MkdirAll(disk, 0o755); err != nil {
			return res, err
		}
		progress("import", "creating the WSL distro "+r.cfg.Distro)
		var out bytes.Buffer
		if exit, err := r.run(ctx, "wsl.exe", []string{"--import", r.cfg.Distro, disk, tarball, "--version", "2"}, nil, &out, &out); err != nil || exit != 0 {
			return res, fmt.Errorf("wsl --import %s failed (exit %d): %v %s\nIf WSL says it needs an update, run wsl --update, then ugt setup again", r.cfg.Distro, exit, err, lastLines(decode(out.Bytes()), 5))
		}
		res.Done = append(res.Done, "created the WSL distro "+r.cfg.Distro+" from Alpine Linux "+r.cfg.AlpineBranch)
	}
	res.Created = "wsl:" + r.cfg.Distro

	progress("provision", "installing Docker Engine, Compose and Buildx in the distro")
	var out bytes.Buffer
	if exit, err := r.inDistro(ctx, "", nil, []string{"sh", "-s"}, strings.NewReader(r.cfg.Provision), &out, &out); err != nil || exit != 0 {
		return res, fmt.Errorf("provisioning the layer failed (exit %d): %v\n%s", exit, err, lastLines(out.String(), 15))
	}
	res.Done = append(res.Done, strings.TrimSpace(lastLines(out.String(), 1)))
	// the provisioned wsl.conf (boot command, no Windows drives) applies from the next start
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

// rootfs downloads Alpine's minimal root file system for this CPU, checked
// against the SHA-256 Alpine publishes with it. A download already there and
// intact is used again, so a slow connection never fetches it twice.
func (r *Runtime) rootfs(ctx context.Context, progress func(string, string)) (string, error) {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	if arch == "" {
		return "", fmt.Errorf("UGT's layer is built for x86-64 and Arm64 PCs, not %s", runtime.GOARCH)
	}
	base := fmt.Sprintf("%s/%s/releases/%s", r.alpine, r.cfg.AlpineBranch, arch)
	index, err := r.get(ctx, base+"/latest-releases.yaml", 1<<20)
	if err != nil {
		return "", err
	}
	var releases []struct {
		Flavor string `yaml:"flavor"`
		File   string `yaml:"file"`
		SHA256 string `yaml:"sha256"`
	}
	if err := yaml.Unmarshal(index, &releases); err != nil {
		return "", fmt.Errorf("read Alpine's release list: %w", err)
	}
	var file, sum string
	for _, rel := range releases {
		if rel.Flavor == "alpine-minirootfs" {
			file, sum = rel.File, rel.SHA256
		}
	}
	if file == "" || path.Base(file) != file || len(sum) != 64 {
		return "", fmt.Errorf("Alpine's release list for %s has no minimal root file system", arch)
	}
	if err := os.MkdirAll(r.cfg.InstallDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(r.cfg.InstallDir, file)
	if have, err := os.ReadFile(dest); err == nil && sha(have) == sum {
		return dest, nil
	}
	progress("download", "downloading "+file+" from Alpine Linux")
	data, err := r.get(ctx, base+"/"+file, 64<<20)
	if err != nil {
		return "", err
	}
	if got := sha(data); got != sum {
		return "", fmt.Errorf("%s has SHA-256 %s, but Alpine publishes %s: not using it", file, got, sum)
	}
	return dest, os.WriteFile(dest, data, 0o644)
}

func (r *Runtime) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download %s: larger than expected", url)
	}
	return data, nil
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

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
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
	host app.Files
	root func(string) string
}

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
