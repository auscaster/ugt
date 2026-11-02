//go:build darwin || linux

// Package vm is UGT's own Linux machine on a Mac: the layer, booted on Apple's
// Virtualization framework by a background ugt process, so a Mac needs nothing
// but UGT. It implements the Runtime port. Programs run in the machine over
// SSH, the standard protocol for terminals, signals and exit codes, which the
// background process carries to the machine's sshd over vsock. The workspaces
// are the Mac's own folder, shared into the machine at the same path, so
// files are the host's and a path means the same on both sides.
package vm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"

	"github.com/auscaster/ugt/internal/app"
)

// Config is the machine contract and where the machine's files live.
type Config struct {
	Machine    string   // the published machine files this ugt boots, by revision
	Revision   string   // their revision, the layer's
	Init       string   // the machine's PID 1, in its root
	Engine     string   // the command that starts the engine and waits for it
	Poweroff   string   // the command that stops the engine, then the machine, cleanly
	Dir        string   // where the machine's files, data disk and keys live
	Workspaces string   // the Mac's folder of recipe workspaces, shared at its own path
	Serve      []string // the command that runs the machine in the background: ugt with ServeFlag
}

// ServeFlag, as ugt's only argument, makes it the background process that
// runs the machine.
const ServeFlag = "--machine"

// Fetch downloads an image's file system for an architecture into dir, as a
// gzipped tar, and returns its path.
type Fetch func(ctx context.Context, image, arch, dir string, progress func(string)) (string, error)

// Runtime is UGT's machine, as every ugt on the Mac reaches it.
type Runtime struct {
	cfg   Config
	files app.Files
	fetch Fetch
}

var _ app.Runtime = (*Runtime)(nil)

// New returns the machine runtime. files is the Files port on the Mac's file
// system, which the machine shares; fetch downloads the machine's files.
func New(cfg Config, files app.Files, fetch Fetch) *Runtime {
	return &Runtime{cfg: cfg, files: files, fetch: fetch}
}

func (r *Runtime) Name() string       { return "vm" }
func (r *Runtime) Workspaces() string { return filepath.ToSlash(r.cfg.Workspaces) }
func (r *Runtime) Files() app.Files   { return r.files }

// Status reports whether UGT's machine is set up for this ugt. It starts the
// machine when it is not running, as anything that needs the engine would.
func (r *Runtime) Status(ctx context.Context) (app.RuntimeStatus, error) {
	if !r.paths().installed() {
		return app.RuntimeStatus{Problem: "UGT's Linux machine is not set up on this Mac.", Fix: "Run ugt setup."}, nil
	}
	got, err := r.ask(ctx, request{Op: opStatus})
	if err != nil {
		return app.RuntimeStatus{Problem: "UGT's Linux machine did not start: " + err.Error(), Fix: "Run ugt setup to repair it."}, nil
	}
	if got.Revision != r.cfg.Revision {
		return app.RuntimeStatus{
			Problem: fmt.Sprintf("UGT's Linux machine is running layer %s, not this ugt's %s.", got.Revision, r.cfg.Revision),
			Fix:     "Run ugt setup to restart it on this ugt's layer.",
		}, nil
	}
	return app.RuntimeStatus{Ready: true}, nil
}

// Setup downloads the machine's files for this ugt's layer once, makes its
// keys and data disk the first time, restarts a machine running another
// layer, and starts it. It needs no admin rights and no restart.
func (r *Runtime) Setup(ctx context.Context, opts app.SetupOptions) (app.SetupResult, error) {
	progress := opts.Progress
	if progress == nil {
		progress = func(string, string) {}
	}
	res := app.SetupResult{Runtime: r.Name(), Created: "vm:UGT"}
	p := r.paths()
	if !p.hasFiles() {
		progress("machine", "downloading UGT's Linux machine, once for this version of UGT")
		if err := r.install(ctx, func(s string) { progress("machine", s) }); err != nil {
			return res, err
		}
		res.Done = append(res.Done, "downloaded UGT's Linux machine, layer "+r.cfg.Revision)
	}
	made, err := p.prepare()
	if err != nil {
		return res, err
	}
	if made {
		res.Done = append(res.Done, "made the machine's data disk and keys")
	}
	if got, err := r.dialOnly(ctx, request{Op: opStatus}); err == nil && got.Revision != r.cfg.Revision {
		progress("machine", "stopping the machine running layer "+got.Revision)
		if err := r.stop(ctx); err != nil {
			return res, err
		}
		res.Done = append(res.Done, "stopped the machine running layer "+got.Revision)
	}
	p.forgetOthers()
	progress("machine", "starting UGT's Linux machine")
	status, err := r.Status(ctx)
	if err != nil {
		return res, err
	}
	if !status.Ready {
		res.Next = []string{status.Problem + " " + status.Fix}
		return res, nil
	}
	res.Ready = true
	res.Done = append(res.Done, "started UGT's Linux machine with Docker Engine")
	return res, nil
}

// Teardown stops the machine and removes everything Setup made: its files,
// its data disk with every image, container and volume, and its keys.
func (r *Runtime) Teardown(ctx context.Context) error {
	if _, err := r.dialOnly(ctx, request{Op: opStatus}); err == nil {
		if err := r.stop(ctx); err != nil {
			return err
		}
	}
	return os.RemoveAll(r.cfg.Dir)
}

// Folder shares a folder of the user's with the machine for one command, at
// its own path there, so the command sees the path the user knows.
func (r *Runtime) Folder(ctx context.Context, dir string) (string, func(), error) {
	if _, err := r.ask(ctx, request{Op: opFolder, Path: dir}); err != nil {
		return "", nil, fmt.Errorf("share %s with UGT's machine: %w", dir, err)
	}
	return dir, func() { _, _ = r.ask(context.WithoutCancel(ctx), request{Op: opRelease, Path: dir}) }, nil
}

// Exec runs a program in the machine over SSH. With a terminal on stdin the
// program gets one too, at the terminal's size.
func (r *Runtime) Exec(ctx context.Context, c app.Command) (int, error) {
	client, err := r.connect(ctx)
	if err != nil {
		return -1, err
	}
	defer client.Close()
	s, err := client.NewSession()
	if err != nil {
		return -1, fmt.Errorf("run %s in UGT's machine: %w", c.Name, err)
	}
	defer s.Close()
	s.Stdin, s.Stdout, s.Stderr = c.Stdin, orDiscard(c.Stdout), orDiscard(c.Stderr)
	if f, ok := c.Stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		restore, err := terminal(s, f)
		if err != nil {
			return -1, err
		}
		defer restore()
	}
	done := make(chan error, 1)
	go func() { done <- s.Run(commandLine(c)) }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = s.Signal(ssh.SIGTERM)
		_ = client.Close()
		<-done
		return -1, ctx.Err()
	}
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		return exit.ExitStatus(), nil
	}
	if err != nil {
		return -1, fmt.Errorf("run %s in UGT's machine: %w", c.Name, err)
	}
	return 0, nil
}

// terminal gives the session a terminal of f's size, follows its size, and
// puts f in raw mode, so keys reach the program as they are typed.
func terminal(s *ssh.Session, f *os.File) (func(), error) {
	fd := int(f.Fd())
	w, h, err := term.GetSize(fd)
	if err != nil {
		return nil, err
	}
	if err := s.RequestPty(termName(), h, w, ssh.TerminalModes{}); err != nil {
		return nil, err
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	go func() {
		for range resized {
			if w, h, err := term.GetSize(fd); err == nil {
				_ = s.WindowChange(h, w)
			}
		}
	}()
	return func() {
		signal.Stop(resized)
		close(resized)
		_ = term.Restore(fd, old)
	}, nil
}

func termName() string {
	if t := os.Getenv("TERM"); t != "" {
		return t
	}
	return "xterm-256color"
}

// commandLine is c as one line for the machine's shell, every word quoted:
// SSH runs a command as a line, not as arguments.
func commandLine(c app.Command) string {
	words := []string{"exec"}
	if len(c.Env) > 0 {
		keys := make([]string, 0, len(c.Env))
		for k := range c.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		words = append(words, "env")
		for _, k := range keys {
			words = append(words, quote(k+"="+c.Env[k]))
		}
	}
	words = append(words, quote(c.Name))
	for _, a := range c.Args {
		words = append(words, quote(a))
	}
	line := strings.Join(words, " ")
	if c.Dir != "" {
		line = "cd " + quote(c.Dir) + " && " + line
	}
	return line
}

// quote makes s one word for a POSIX shell.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// connect opens an SSH connection to the machine, starting it if needed.
func (r *Runtime) connect(ctx context.Context) (*ssh.Client, error) {
	conn, _, err := r.open(ctx, request{Op: opSSH})
	if err != nil {
		return nil, err
	}
	p := r.paths()
	config, err := p.clientConfig()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	sc, chans, reqs, err := ssh.NewClientConn(conn, "ugt", config)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("reach UGT's machine: %w", err)
	}
	return ssh.NewClient(sc, chans, reqs), nil
}

// ask sends one request to the machine, starting it if needed, and returns
// its answer.
func (r *Runtime) ask(ctx context.Context, req request) (response, error) {
	conn, got, err := r.open(ctx, req)
	if err != nil {
		return response{}, err
	}
	_ = conn.Close()
	return got, nil
}

// dialOnly asks a machine that is running already, and never starts one.
func (r *Runtime) dialOnly(ctx context.Context, req request) (response, error) {
	conn, got, err := r.dial(ctx, req)
	if err != nil {
		return response{}, err
	}
	_ = conn.Close()
	return got, nil
}

// open sends req to the machine, starting it first when no machine answers.
// Starting holds a lock, so ugts started together start one machine.
func (r *Runtime) open(ctx context.Context, req request) (net.Conn, response, error) {
	if conn, got, err := r.dial(ctx, req); !errors.Is(err, errNotRunning) {
		return conn, got, err
	}
	unlock, err := r.paths().lock("start.lock", true)
	if err != nil {
		return nil, response{}, err
	}
	defer unlock()
	if conn, got, err := r.dial(ctx, req); !errors.Is(err, errNotRunning) {
		return conn, got, err
	}
	if err := r.start(); err != nil {
		return nil, response{}, err
	}
	return r.dial(ctx, req)
}

// errNotRunning is a dial that found no machine.
var errNotRunning = errors.New("UGT's machine is not running")

// dial sends req to a running machine and reads its answer. The connection
// stays open: after an ssh request it carries the SSH protocol.
func (r *Runtime) dial(ctx context.Context, req request) (net.Conn, response, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", r.paths().socket())
	if err != nil {
		return nil, response{}, errNotRunning
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		_ = conn.Close()
		return nil, response{}, errNotRunning
	}
	in := bufio.NewReader(conn)
	line, err := in.ReadBytes('\n')
	if err != nil {
		// a machine stopping as it was reached: it is not running
		_ = conn.Close()
		return nil, response{}, errNotRunning
	}
	var got response
	if err := json.Unmarshal(line, &got); err != nil {
		_ = conn.Close()
		return nil, response{}, fmt.Errorf("UGT's machine answered %q: %w", line, err)
	}
	if got.Error != "" {
		_ = conn.Close()
		return nil, got, errors.New(got.Error)
	}
	return buffered{conn, in}, got, nil
}

// buffered is a connection whose first bytes were read into in.
type buffered struct {
	net.Conn
	in *bufio.Reader
}

func (b buffered) Read(p []byte) (int, error) { return b.in.Read(p) }

// start runs the machine in a background ugt, which outlives this one, and
// waits until its engine is ready or it has said why it could not start.
func (r *Runtime) start() error {
	p := r.paths()
	if err := os.MkdirAll(r.cfg.Dir, 0o700); err != nil {
		return err
	}
	log, err := os.Create(p.log())
	if err != nil {
		return err
	}
	defer log.Close()
	ready, tell, err := os.Pipe()
	if err != nil {
		return err
	}
	defer ready.Close()
	cmd := exec.Command(r.cfg.Serve[0], r.cfg.Serve[1:]...)
	cmd.Stdout, cmd.Stderr, cmd.ExtraFiles = log, log, []*os.File{tell}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // its own session: a terminal or AI app closing does not stop it
	err = cmd.Start()
	_ = tell.Close()
	if err != nil {
		return fmt.Errorf("start UGT's machine: %w", err)
	}
	_ = cmd.Process.Release()
	said, _ := io.ReadAll(ready)
	if msg := strings.TrimSpace(string(said)); msg != readyLine {
		if msg == "" {
			msg = "it stopped: " + lastLines(p.log(), 5)
		}
		return errors.New(msg)
	}
	return nil
}

// stop asks the machine to power off and waits until it has.
func (r *Runtime) stop(ctx context.Context) error {
	if _, err := r.dialOnly(ctx, request{Op: opStop}); err != nil && !errors.Is(err, errNotRunning) {
		return fmt.Errorf("stop UGT's machine: %w", err)
	}
	return nil
}

func lastLines(path string, n int) string {
	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " / ")
}

func orDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

// handshake bounds connecting to the machine's sshd, which answers at once.
const handshake = 30 * time.Second
