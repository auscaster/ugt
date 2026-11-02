//go:build darwin || linux

package vm

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/auscaster/ugt/internal/app"
)

// TestMain lets the test binary be the background process that runs the
// machine: a fake one, which answers on the machine's socket and runs the
// commands SSH brings on this computer.
func TestMain(m *testing.M) {
	if dir := os.Getenv("UGT_FAKE_MACHINE"); dir != "" {
		os.Exit(fakeMachine(dir, os.Getenv("UGT_FAKE_REVISION")))
	}
	os.Exit(m.Run())
}

// newRuntime is a runtime on a machine whose files are all in place, which
// the test binary runs as revision.
func newRuntime(t *testing.T, revision string) *Runtime {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("UGT_FAKE_MACHINE", dir)
	t.Setenv("UGT_FAKE_REVISION", revision)
	r := New(Config{Machine: "example/machine:" + revision, Revision: revision, Dir: dir, Workspaces: filepath.Join(dir, "workspaces"),
		Serve: []string{os.Args[0]}}, nil, nil)
	p := r.paths()
	if err := os.MkdirAll(filepath.Join(dir, revision), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range machineFiles {
		if err := os.WriteFile(p.file(f), []byte(f), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.prepare(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.stop(context.Background()) })
	return r
}

// A command reaches the machine's shell word for word, in its folder, with
// its environment and input, and its exit code comes back.
func TestExecRunsTheCommandAsGiven(t *testing.T) {
	r := newRuntime(t, "abc123")
	dir := filepath.Join(t.TempDir(), "it's a folder")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	exit, err := r.Exec(context.Background(), app.Command{
		Name: "sh", Args: []string{"-c", `printf '%s|' "$1" "$(pwd)" "$GREETING" "$(cat)"; echo oops >&2; exit 3`, "sh", `a "quoted" $word; and more`},
		Dir: dir, Env: map[string]string{"GREETING": "it's me"}, Stdin: strings.NewReader("from stdin"), Stdout: &out, Stderr: &errOut,
	})
	if err != nil || exit != 3 {
		t.Fatalf("exit %d, %v; stderr %q", exit, err, errOut.String())
	}
	if want := `a "quoted" $word; and more|` + dir + `|it's me|from stdin|`; out.String() != want {
		t.Errorf("got %q\nwant %q", out.String(), want)
	}
	if errOut.String() != "oops\n" {
		t.Errorf("stderr %q", errOut.String())
	}
}

// ugts that need the machine at once start one machine between them.
func TestUgtsStartedTogetherStartOneMachine(t *testing.T) {
	r := newRuntime(t, "abc123")
	var wg sync.WaitGroup
	statuses := make([]app.RuntimeStatus, 4)
	for i := range statuses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i], _ = r.Status(context.Background())
		}()
	}
	wg.Wait()
	for _, s := range statuses {
		if !s.Ready {
			t.Fatalf("not ready: %+v", s)
		}
	}
	if starts, _ := os.ReadFile(filepath.Join(r.cfg.Dir, "starts")); string(starts) != "abc123\n" {
		t.Errorf("machines started: %q", starts)
	}
}

// A machine that cannot start says why, and that is what the user is told.
func TestAMachineThatCannotStartSaysWhy(t *testing.T) {
	r := newRuntime(t, "abc123")
	t.Setenv("UGT_FAKE_REVISION", "fail")
	s, err := r.Status(context.Background())
	if err != nil || s.Ready || !strings.Contains(s.Problem, "not signed to run virtual machines") || s.Fix != "Run ugt setup to repair it." {
		t.Fatalf("%+v, %v", s, err)
	}
}

// Setup puts a revision's machine files in place only whole, keeps the data
// disk, restarts a machine running another layer, and forgets the old files.
func TestSetupInstallsTheMachineAndRestartsAnOlderOne(t *testing.T) {
	old := newRuntime(t, "old111")
	if s, _ := old.Status(context.Background()); !s.Ready {
		t.Fatalf("old machine: %+v", s)
	}
	disk, _ := os.Stat(old.paths().disk())

	var files map[string]string
	r := New(Config{Machine: "example/machine:new222", Revision: "new222", Dir: old.cfg.Dir, Workspaces: old.cfg.Workspaces, Serve: old.cfg.Serve}, nil,
		func(_ context.Context, image, arch, dir string, _ func(string)) (string, error) {
			return machineTarball(t, dir, files), nil
		})
	if s, _ := r.Status(context.Background()); s.Ready || s.Fix != "Run ugt setup." {
		t.Fatalf("before setup: %+v", s)
	}
	files = map[string]string{kernelFile: "k", initramfsFile: "i"}
	if _, err := r.Setup(context.Background(), app.SetupOptions{}); err == nil || !strings.Contains(err.Error(), "no root.squashfs") {
		t.Fatalf("a download without the root: %v", err)
	}
	if exists(filepath.Join(r.cfg.Dir, "new222")) {
		t.Fatal("a half download was kept")
	}

	files = map[string]string{kernelFile: "k", initramfsFile: "i", rootFile: "r"}
	t.Setenv("UGT_FAKE_REVISION", "new222")
	res, err := r.Setup(context.Background(), app.SetupOptions{})
	if err != nil || !res.Ready || res.Created != "vm:UGT" {
		t.Fatalf("%+v, %v", res, err)
	}
	if got := strings.Join(res.Done, "; "); got != "downloaded UGT's Linux machine, layer new222; stopped the machine running layer old111; started UGT's Linux machine with Docker Engine" {
		t.Errorf("done: %s", got)
	}
	if root, _ := os.ReadFile(r.paths().file(rootFile)); string(root) != "r" || exists(filepath.Join(r.cfg.Dir, "old111")) {
		t.Errorf("root %q; old files kept: %v", root, exists(filepath.Join(r.cfg.Dir, "old111")))
	}
	if again, _ := os.Stat(r.paths().disk()); !os.SameFile(disk, again) || again.Size() != diskSize {
		t.Errorf("the data disk was not kept")
	}
	if starts, _ := os.ReadFile(filepath.Join(r.cfg.Dir, "starts")); string(starts) != "old111\nnew222\n" {
		t.Errorf("machines started: %q", starts)
	}
}

// A command's folder is shared for that command and released after it.
func TestFolderIsReleasedAfterTheCommand(t *testing.T) {
	r := newRuntime(t, "abc123")
	path, release, err := r.Folder(context.Background(), "/Users/me/project")
	if err != nil || path != "/Users/me/project" {
		t.Fatalf("%q, %v", path, err)
	}
	release()
	if got, _ := os.ReadFile(filepath.Join(r.cfg.Dir, "requests")); string(got) != "folder /Users/me/project\nrelease /Users/me/project\n" {
		t.Errorf("requests: %q", got)
	}
}

func machineTarball(t *testing.T, dir string, files map[string]string) string {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		_ = tw.WriteHeader(&tar.Header{Name: "./" + name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(data))
	}
	_ = tw.Close()
	_ = gz.Close()
	path := filepath.Join(dir, "layer.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeMachine is the background process, with this computer as its machine.
func fakeMachine(dir, revision string) int {
	ready := os.NewFile(3, "ready")
	p := paths{dir, revision}
	unlock, err := p.lock("machine.lock", false)
	if err != nil {
		_ = tell(ready, errors.New("running in another process already"))
		return 1
	}
	defer unlock()
	if revision == "fail" {
		_ = tell(ready, errors.New("start UGT's machine: no entitlement: this ugt is not signed to run virtual machines"))
		return 1
	}
	record := func(file, line string) {
		f, _ := os.OpenFile(filepath.Join(dir, file), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		fmt.Fprintln(f, line)
		f.Close()
	}
	record("starts", revision)
	server, err := sshServer(p)
	if err != nil {
		_ = tell(ready, err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(p.socket()), 0o700); err != nil {
		_ = tell(ready, err)
		return 1
	}
	_ = os.Remove(p.socket())
	ln, err := net.Listen("unix", p.socket())
	if err != nil {
		_ = tell(ready, err)
		return 1
	}
	_, _ = fmt.Fprintln(ready, readyLine)
	_ = ready.Close()
	stop := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				in := bufio.NewReader(conn)
				line, _ := in.ReadBytes('\n')
				var req request
				_ = json.Unmarshal(line, &req)
				_ = json.NewEncoder(conn).Encode(response{Revision: revision})
				switch req.Op {
				case opSSH:
					serveSSH(buffered{conn, in}, server)
				case opFolder, opRelease:
					record("requests", req.Op+" "+req.Path)
				case opStop:
					_ = ln.Close()
					_ = os.Remove(p.socket())
					close(stop)
				}
			}()
		}
	}()
	<-stop
	return 0
}

// sshServer accepts UGT's key only, and shows the machine's own host key.
func sshServer(p paths) (*ssh.ServerConfig, error) {
	hostPEM, err := os.ReadFile(filepath.Join(p.share(), "ssh_host_ed25519_key"))
	if err != nil {
		return nil, err
	}
	host, err := ssh.ParsePrivateKey(hostPEM)
	if err != nil {
		return nil, err
	}
	authorized, err := os.ReadFile(filepath.Join(p.share(), "authorized_keys"))
	if err != nil {
		return nil, err
	}
	allowed, _, _, _, err := ssh.ParseAuthorizedKey(authorized)
	if err != nil {
		return nil, err
	}
	config := &ssh.ServerConfig{PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if meta.User() == "root" && bytes.Equal(key.Marshal(), allowed.Marshal()) {
			return nil, nil
		}
		return nil, errors.New("not UGT's key")
	}}
	config.AddHostKey(host)
	return config, nil
}

// serveSSH runs each command a session asks for with sh, as sshd does.
func serveSSH(conn net.Conn, config *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		session, requests, err := ch.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer session.Close()
			for req := range requests {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				line := string(req.Payload[4:])
				_ = req.Reply(true, nil)
				cmd := exec.Command("sh", "-c", line)
				cmd.Stdin, cmd.Stdout, cmd.Stderr = session, session, session.Stderr()
				code := 0
				if err := cmd.Run(); err != nil {
					var exit *exec.ExitError
					if !errors.As(err, &exit) {
						code = 127
					} else {
						code = exit.ExitCode()
					}
				}
				status := make([]byte, 4)
				binary.BigEndian.PutUint32(status, uint32(code))
				_, _ = session.SendRequest("exit-status", false, status)
				return
			}
		}()
	}
}
