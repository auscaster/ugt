package wsl

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/auscaster/ugt/internal/app"
)

// fakeWSL answers wsl.exe the way Windows does, from a little state.
type fakeWSL struct {
	installed bool
	distros   []string
	engineUp  bool
	utf16     bool // answer --list in UTF-16, as older WSL does
	ran       []string
	stdin     string
}

func (w *fakeWSL) run(_ context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	line := strings.Join(args, " ")
	w.ran = append(w.ran, line)
	if stdin != nil {
		data, _ := io.ReadAll(stdin)
		w.stdin = string(data)
	}
	if name != "wsl.exe" {
		return -1, fmt.Errorf("ran %s", name)
	}
	switch {
	case line == "--status":
		if !w.installed {
			return 1, nil
		}
	case line == "--install --no-distribution":
		w.installed = true
	case line == "--list --quiet":
		out := strings.Join(w.distros, "\r\n") + "\r\n"
		if w.utf16 {
			fmt.Fprint(stdout, utf16le(out))
		} else {
			fmt.Fprint(stdout, out)
		}
	case strings.HasPrefix(line, "--import UGT "):
		w.distros = append(w.distros, "UGT")
	case strings.HasSuffix(line, "--exec sh -s"):
		fmt.Fprintln(stdout, "ugt layer: Docker version 29.9.2")
	case strings.HasSuffix(line, "--exec /usr/local/bin/ugt-engine"):
		if !w.engineUp {
			fmt.Fprintln(stdout, "dockerd did not start")
			return 1, nil
		}
	}
	return 0, nil
}

func utf16le(s string) string {
	var b strings.Builder
	b.WriteString("\xff\xfe")
	for _, u := range utf16.Encode([]rune(s)) {
		b.WriteByte(byte(u))
		b.WriteByte(byte(u >> 8))
	}
	return b.String()
}

func newRuntime(t *testing.T, w *fakeWSL) (*Runtime, *int) {
	t.Helper()
	tarball := []byte("a root file system")
	downloads := 0
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	mux := http.NewServeMux()
	mux.HandleFunc("/v3.24/releases/"+arch+"/latest-releases.yaml", func(rw http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(rw, "-\n  flavor: alpine-netboot\n  file: netboot.tar.gz\n  sha256: %s\n-\n  flavor: alpine-minirootfs\n  file: alpine-minirootfs-3.24.3-%s.tar.gz\n  sha256: %s\n",
			strings.Repeat("0", 64), arch, sha(tarball))
	})
	mux.HandleFunc("/v3.24/releases/"+arch+"/alpine-minirootfs-3.24.3-"+arch+".tar.gz", func(rw http.ResponseWriter, _ *http.Request) {
		downloads++
		rw.Write(tarball)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	r := New(Config{
		Distro: "UGT", Workspaces: "/var/lib/ugt/workspaces", Engine: "/usr/local/bin/ugt-engine",
		Provision: "echo provision\n", AlpineBranch: "v3.24", InstallDir: t.TempDir(),
	}, nil)
	r.run, r.alpine = w.run, srv.URL
	return r, &downloads
}

func TestSetupNeedsConsentToInstallWSL(t *testing.T) {
	w := &fakeWSL{}
	r, _ := newRuntime(t, w)
	res, err := r.Setup(context.Background(), app.SetupOptions{})
	if err != nil || res.Ready || !strings.Contains(strings.Join(res.Next, " "), "ugt setup --yes") {
		t.Fatalf("without consent: %+v, %v", res, err)
	}
	if strings.Contains(strings.Join(w.ran, "\n"), "--install") {
		t.Fatal("installed WSL without consent")
	}
	res, err = r.Setup(context.Background(), app.SetupOptions{AllowAdmin: true})
	if err != nil || !res.NeedsRestart || !w.installed {
		t.Fatalf("with consent: %+v, %v", res, err)
	}
}

func TestSetupImportsAndProvisions(t *testing.T) {
	w := &fakeWSL{installed: true, distros: []string{"Ubuntu"}, engineUp: true, utf16: true}
	r, downloads := newRuntime(t, w)
	res, err := r.Setup(context.Background(), app.SetupOptions{})
	if err != nil || !res.Ready {
		t.Fatalf("setup %+v, %v (ran %q)", res, err, w.ran)
	}
	ran := strings.Join(w.ran, "\n")
	for _, want := range []string{
		"--import UGT " + filepath.Join(r.cfg.InstallDir, "disk") + " " + filepath.Join(r.cfg.InstallDir, "alpine-minirootfs-3.24.3-"),
		"--distribution UGT --user root --exec sh -s",
		"--terminate UGT",
		"--distribution UGT --user root --exec /usr/local/bin/ugt-engine",
	} {
		if !strings.Contains(ran, want) {
			t.Errorf("setup did not run %q:\n%s", want, ran)
		}
	}
	if w.stdin != "echo provision\n" {
		t.Errorf("the provisioning script went in as %q", w.stdin)
	}

	// running setup again repairs without importing or downloading again
	w.ran = nil
	if res, err := r.Setup(context.Background(), app.SetupOptions{}); err != nil || !res.Ready {
		t.Fatalf("second setup %+v, %v", res, err)
	}
	if strings.Contains(strings.Join(w.ran, "\n"), "--import") || *downloads != 1 {
		t.Errorf("second setup imported again or downloaded again (%d downloads): %q", *downloads, w.ran)
	}
}

func TestSetupReusesAnIntactDownload(t *testing.T) {
	w := &fakeWSL{installed: true, engineUp: true}
	r, downloads := newRuntime(t, w)
	if _, err := r.rootfs(context.Background(), func(string, string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.rootfs(context.Background(), func(string, string) {}); err != nil {
		t.Fatal(err)
	}
	if *downloads != 1 {
		t.Errorf("downloaded %d times", *downloads)
	}
	// a damaged file is downloaded again
	matches, _ := filepath.Glob(filepath.Join(r.cfg.InstallDir, "alpine-minirootfs-*"))
	os.WriteFile(matches[0], []byte("damaged"), 0o644)
	if _, err := r.rootfs(context.Background(), func(string, string) {}); err != nil || *downloads != 2 {
		t.Errorf("after damage: %v, %d downloads", err, *downloads)
	}
}

func TestStatus(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		w     *fakeWSL
		ready bool
		want  string
	}{
		{&fakeWSL{}, false, "WSL, which UGT runs its engine in, is not installed"},
		{&fakeWSL{installed: true, distros: []string{"Ubuntu"}}, false, "not set up yet"},
		{&fakeWSL{installed: true, distros: []string{"UGT"}}, false, "did not start"},
		{&fakeWSL{installed: true, distros: []string{"UGT"}, engineUp: true, utf16: true}, true, ""},
	} {
		r, _ := newRuntime(t, c.w)
		s, err := r.Status(ctx)
		if err != nil || s.Ready != c.ready || !strings.Contains(s.Problem, c.want) {
			t.Errorf("%+v: %+v, %v", c.w, s, err)
		}
	}
}

// Programs run through wsl.exe --exec, so no shell parses their arguments,
// with the environment through env(1) in a stable order.
func TestExec(t *testing.T) {
	w := &fakeWSL{installed: true}
	r, _ := newRuntime(t, w)
	_, err := r.Exec(context.Background(), app.Command{
		Name: "docker", Args: []string{"compose", "run", "--rm", "task"},
		Dir: "/var/lib/ugt/workspaces/a/b/c", Env: map[string]string{"UGT_IMAGE": "ugt/a/b/c:1", "COMPOSE_PROJECT_NAME": "ugt-a-b-c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "--distribution UGT --user root --cd /var/lib/ugt/workspaces/a/b/c --exec env COMPOSE_PROJECT_NAME=ugt-a-b-c UGT_IMAGE=ugt/a/b/c:1 docker compose run --rm task"
	if got := w.ran[len(w.ran)-1]; got != want {
		t.Errorf("ran\n %s\nwant\n %s", got, want)
	}
}

func TestDecode(t *testing.T) {
	if got := decode([]byte(utf16le("UGT\r\nUbuntu\r\n"))); got != "UGT\nUbuntu\n" {
		t.Errorf("utf-16: %q", got)
	}
	if got := decode([]byte("UGT\r\n")); got != "UGT\n" {
		t.Errorf("utf-8: %q", got)
	}
}
