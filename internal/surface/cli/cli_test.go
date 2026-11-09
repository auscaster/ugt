package cli

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/usegeekthing/ugt/internal/app"
)

const examples = "../../../spec/recipe/v0/examples/"

type noInstalledApps struct{ err error }

func (a noInstalledApps) Find(context.Context) ([]app.AgentHost, error) { return nil, a.err }
func (noInstalledApps) Connect(context.Context, string, app.MCPServer) (string, error) {
	return "", fs.ErrNotExist
}
func (noInstalledApps) Disconnect(context.Context, string) (string, error) {
	return "", fs.ErrNotExist
}

func TestConnectWithoutAnAppKeepsBrowserUseAvailable(t *testing.T) {
	for _, tc := range []struct {
		args []string
		err  error
		want int
	}{
		{[]string{"connect"}, nil, ExitOK},
		{[]string{"connect", "cursor"}, nil, ExitFailed},
		{[]string{"connect"}, fs.ErrPermission, ExitFailed},
	} {
		var out, stderr bytes.Buffer
		c := &CLI{App: &app.App{Agents: noInstalledApps{tc.err}}, Stdout: &out, Stderr: &stderr}
		if got := c.Run(context.Background(), tc.args); got != tc.want {
			t.Fatalf("%v: exit %d, want %d: %s", tc.args, got, tc.want, &stderr)
		}
		if tc.want == ExitOK && (stderr.Len() != 0 || !strings.Contains(out.String(), "browser")) {
			t.Fatalf("successful installation should lead back to the browser: %s, %s", &out, &stderr)
		}
	}
}

// readDisk is a Disk that only reads, enough for validate.
type readDisk struct{}

func (readDisk) TryLock(string) (func(), error) { return nil, fs.ErrPermission }

func (readDisk) FS(dir string) fs.FS            { return os.DirFS(dir) }
func (readDisk) WriteFile(string, []byte) error { return fs.ErrPermission }
func (readDisk) RemoveAll(string) error         { return fs.ErrPermission }
func (readDisk) Exists(string) (bool, error)    { return false, nil }

func run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	c := &CLI{App: &app.App{Disk: readDisk{}}, Stdout: &stdout, Stderr: &stderr, Version: "test", Layer: "ghcr.io/auscaster/ugt/layer:abc"}
	status := c.Run(context.Background(), args)
	return status, stdout.String(), stderr.String()
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		status int
		out    string
	}{
		{"valid", []string{"recipe", "validate", examples + "valid/example-job"}, ExitOK, "ok"},
		{"recipe.json path", []string{"recipe", "validate", examples + "valid/example-job/recipe.json"}, ExitOK, "ok"},
		{"invalid", []string{"recipe", "validate", examples + "invalid/missing-check-service"}, ExitFailed, "services.check is missing"},
		{"missing", []string{"recipe", "validate", examples + "nope"}, ExitFailed, "invalid"},
		{"mixed", []string{"recipe", "validate", examples + "valid/example-job", examples + "invalid/short-commit"}, ExitFailed, "invalid"},
		{"no folders", []string{"recipe", "validate"}, ExitUsage, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, out, _ := run(c.args...)
			if status != c.status || !strings.Contains(out, c.out) {
				t.Fatalf("status %d, output %q", status, out)
			}
		})
	}
}

func TestUsage(t *testing.T) {
	status, _, errOut := run("frobnicate")
	if status != ExitUsage || !strings.Contains(errOut, "ugt recipe verify") || !strings.Contains(errOut, "ugt recipe validate") {
		t.Fatalf("status %d, usage %q", status, errOut)
	}
	if status, out, _ := run("version"); status != ExitOK || out != "ugt test\nlayer ghcr.io/auscaster/ugt/layer:abc\n" {
		t.Fatalf("version: %d %q", status, out)
	}
}

func TestPulls(t *testing.T) {
	p := pulls{}
	for _, v := range []string{"localhost:5001/x@sha256:1", "-api=ghcr.io/y@sha256:2"} {
		if err := p.Set(v); err != nil {
			t.Fatal(err)
		}
	}
	if p[""] != "localhost:5001/x@sha256:1" || p["-api"] != "ghcr.io/y@sha256:2" {
		t.Fatalf("pulls %v", p)
	}
	for _, bad := range []string{"api=ref", "-api="} {
		if err := (pulls{}).Set(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestVerifyNeedsOneFolder(t *testing.T) {
	if status, _, _ := run("recipe", "verify"); status != ExitUsage {
		t.Fatalf("status %d", status)
	}
}
