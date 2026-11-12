// Package install tests UGT's install scripts, install/install.sh and
// install/install.ps1, against a release served on this computer.
package install

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestMain lets the test binary be the ugt a script installs: it notes each
// command the script runs it with.
func TestMain(m *testing.M) {
	if log := os.Getenv("UGT_FAKE_LOG"); log != "" {
		f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintln(f, strings.Join(os.Args[1:], " "))
			f.Close()
		}
		if len(os.Args) > 1 && os.Args[1] == "version" {
			fmt.Println("ugt 9.9.9")
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// release serves ugt for this computer as the newest release, with the
// digests the release lists for it.
func release(t *testing.T, name, listed string) (url string, ugt []byte) {
	t.Helper()
	ugt, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if listed == "" {
		sum := sha256.Sum256(ugt)
		listed = hex.EncodeToString(sum[:])
	}
	sums := listed + "  " + name + "\n" + strings.Repeat("0", 64) + "  ugt-plan9-mips\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/latest/download/SHA256SUMS":
			_, _ = w.Write([]byte(sums))
		case "/latest/download/" + name:
			_, _ = w.Write(ugt)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, ugt
}

// run runs an install script as the user does, from their own shell: so
// Windows PowerShell gets no PSModulePath, which one started from PowerShell
// 7's, as CI's steps are, would point at modules it cannot load.
func run(t *testing.T, cmd *exec.Cmd, url, dir, log string) (string, error) {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(strings.ToUpper(kv), "PSMODULEPATH=") })
	cmd.Env = append(env, "UGT_RELEASES="+url, "UGT_INSTALL_DIR="+dir, "UGT_FAKE_LOG="+log)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// The install script puts ugt in place only when it is the listed file, then
// sets UGT up and connects AI apps.
func TestInstallScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is for a Mac or Linux")
	}
	name := "ugt-" + runtime.GOOS + "-" + runtime.GOARCH
	url, ugt := release(t, name, "")
	dir, log := filepath.Join(t.TempDir(), "bin"), filepath.Join(t.TempDir(), "calls")
	out, err := run(t, exec.Command("sh", "../../../install/install.sh"), url, dir, log)
	if err != nil || !strings.Contains(out, "Installed ugt 9.9.9 in "+dir) {
		t.Fatalf("%v\n%s", err, out)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "ugt")); string(got) != string(ugt) {
		t.Error("the installed ugt is not the release's")
	}
	if calls, _ := os.ReadFile(log); string(calls) != "version\nconnect\nsetup\n" {
		t.Errorf("ran ugt with %q", calls)
	}

	url, _ = release(t, name, strings.Repeat("ab", 32))
	dir = filepath.Join(t.TempDir(), "bin")
	out, err = run(t, exec.Command("sh", "../../../install/install.sh"), url, dir, filepath.Join(t.TempDir(), "calls"))
	if err == nil || !strings.Contains(out, "does not match the release's digest") {
		t.Fatalf("a download that does not match: %v\n%s", err, out)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}

// The same for Windows PowerShell's script. It adds its folder to the user's
// PATH, so it runs only in CI, on a runner nothing else uses.
func TestInstallScriptForWindows(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("CI") == "" {
		t.Skip("install.ps1 runs on a Windows CI runner")
	}
	name := "ugt-windows-" + runtime.GOARCH + ".exe"
	url, ugt := release(t, name, "")
	dir, log := filepath.Join(t.TempDir(), "bin"), filepath.Join(t.TempDir(), "calls")
	out, err := run(t, exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", `..\..\..\install\install.ps1`), url, dir, log)
	if err != nil || !strings.Contains(out, "Installed ugt 9.9.9 in "+dir) {
		t.Fatalf("%v\n%s", err, out)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "ugt.exe")); string(got) != string(ugt) {
		t.Error("the installed ugt is not the release's")
	}
	if calls, _ := os.ReadFile(log); strings.ReplaceAll(string(calls), "\r", "") != "version\nconnect\nsetup --yes\n" {
		t.Errorf("ran ugt with %q", calls)
	}
}
