package wsl

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/usegeekthing/ugt/internal/app"
)

// fakeWSL answers wsl.exe the way Windows does, from a little state.
type fakeWSL struct {
	installed bool
	distros   []string
	revision  string // the layer revision UGT's distro records
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
	case line == "--version":
		return 1, nil // the WSL that ships with Windows has no --version
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
		w.distros, w.revision = append(w.distros, "UGT"), "layer2"
	case strings.HasSuffix(line, "--exec cat /usr/share/ugt/revision"):
		if w.revision == "" {
			return 1, nil
		}
		fmt.Fprintln(stdout, w.revision)
	case strings.Contains(line, "--exec sh -c"):
		w.revision = "layer2"
		fmt.Fprintln(stdout, "ugt layer: layer2")
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

// newRuntime is the runtime for layer2, with the downloads it asks for.
func newRuntime(t *testing.T, w *fakeWSL) (*Runtime, *[]string) {
	t.Helper()
	var fetched []string
	fetch := func(_ context.Context, image, arch, dir string, progress func(string)) (string, error) {
		fetched = append(fetched, image+" "+arch)
		p := filepath.Join(dir, "layer2.tar.gz")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		return p, os.WriteFile(p, []byte("layer2's file system"), 0o644)
	}
	r := New(Config{
		Distro: "UGT", Workspaces: "/var/lib/ugt/workspaces", Engine: "/usr/local/bin/ugt-engine",
		Layer: "ghcr.io/auscaster/ugt/layer:layer2", Revision: "layer2", RevisionFile: "/usr/share/ugt/revision",
		Update: "update the layer", InstallDir: t.TempDir(),
	}, nil, fetch)
	r.run = w.run
	return r, &fetched
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

func TestSetupImportsTheLayer(t *testing.T) {
	w := &fakeWSL{installed: true, distros: []string{"Ubuntu"}, engineUp: true, utf16: true}
	r, fetched := newRuntime(t, w)
	res, err := r.Setup(context.Background(), app.SetupOptions{})
	if err != nil || !res.Ready {
		t.Fatalf("setup %+v, %v (ran %q)", res, err, w.ran)
	}
	ran := strings.Join(w.ran, "\n")
	for _, want := range []string{
		"--import UGT " + filepath.Join(r.cfg.InstallDir, "disk") + " " + filepath.Join(r.cfg.InstallDir, "layers", "layer2.tar.gz") + " --version 2",
		"--terminate UGT",
		"--distribution UGT --user root --exec /usr/local/bin/ugt-engine",
	} {
		if !strings.Contains(ran, want) {
			t.Errorf("setup did not run %q:\n%s", want, ran)
		}
	}
	if strings.Join(*fetched, ",") != "ghcr.io/auscaster/ugt/layer:layer2 "+runtime.GOARCH {
		t.Errorf("fetched %q", *fetched)
	}

	// running setup again repairs without importing or downloading again
	w.ran = nil
	if res, err := r.Setup(context.Background(), app.SetupOptions{}); err != nil || !res.Ready {
		t.Fatalf("second setup %+v, %v", res, err)
	}
	if strings.Contains(strings.Join(w.ran, "\n"), "--import") || len(*fetched) != 1 {
		t.Errorf("second setup imported or downloaded again (%q): %q", *fetched, w.ran)
	}
}

// A distro on another layer, or one that records none, is updated in place
// from the new layer's file system: nothing is imported again, so its images
// and saved data stay.
func TestSetupUpdatesAnotherLayerInPlace(t *testing.T) {
	for _, had := range []string{"layer1", ""} {
		w := &fakeWSL{installed: true, distros: []string{"UGT"}, revision: had, engineUp: true}
		r, fetched := newRuntime(t, w)
		if s, _ := r.Status(context.Background()); s.Ready || !strings.Contains(s.Fix, "ugt setup") {
			t.Fatalf("status on %q: %+v", had, s)
		}
		res, err := r.Setup(context.Background(), app.SetupOptions{})
		if err != nil || !res.Ready || !strings.Contains(strings.Join(res.Done, " "), "keeping every image and saved data") {
			t.Fatalf("setup from %q: %+v, %v (ran %q)", had, res, err, w.ran)
		}
		ran := strings.Join(w.ran, "\n")
		if strings.Contains(ran, "--import") || !strings.Contains(ran, "--exec sh -c update the layer") || w.stdin != "layer2's file system" || len(*fetched) != 1 {
			t.Errorf("from %q ran %q with %q on stdin, fetched %q", had, ran, w.stdin, *fetched)
		}
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
		{&fakeWSL{installed: true, distros: []string{"UGT"}, revision: "layer1", engineUp: true}, false, `runs layer "layer1"`},
		{&fakeWSL{installed: true, distros: []string{"UGT"}, revision: "layer2"}, false, "did not start"},
		{&fakeWSL{installed: true, distros: []string{"UGT"}, revision: "layer2", engineUp: true, utf16: true}, true, ""},
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

// A command's folder is mounted into the distro for the command alone, and
// nothing of Windows stays mounted after.
func TestFolderIsMountedForOneCommand(t *testing.T) {
	w := &fakeWSL{installed: true, distros: []string{"UGT"}}
	r, _ := newRuntime(t, w)
	at, release, err := r.Folder(context.Background(), `C:\Users\me\docs`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(at, "/mnt/ugt/") {
		t.Errorf("mounted at %s", at)
	}
	release()
	got := strings.Join(w.ran, "\n")
	for _, want := range []string{"--exec mkdir -p " + at, `--exec mount -t drvfs C:\Users\me\docs ` + at, "--exec umount " + at, "--exec rmdir " + at} {
		if !strings.Contains(got, want) {
			t.Errorf("did not run %q:\n%s", want, got)
		}
	}
}
