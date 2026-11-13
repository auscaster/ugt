package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows/registry"
)

const packageKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\UseTheGeekThing`

// Native package qualification touches the runner's own PATH, package record
// and Start menu. It never runs on a developer machine. The downloaded payload
// is TestMain's fake helper, so these checks cannot touch WSL or credentials.
func TestWindowsSetupPackage(t *testing.T) {
	installer := os.Getenv("UGT_TEST_INSTALLER")
	if os.Getenv("GITHUB_ACTIONS") != "true" || installer == "" {
		t.Skip("the package runs only on its disposable Windows Actions runner")
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, packageKey, registry.READ); err == nil {
		k.Close()
		t.Fatal("a pre-existing UGT package must not be replaced by qualification")
	}
	for _, state := range []string{"ready", "restart", "failed", "bad-digest"} {
		t.Run(state, func(t *testing.T) {
			sum := ""
			if state == "bad-digest" {
				sum = strings.Repeat("ab", 32)
			}
			url, _ := release(t, "ugt-windows-amd64.exe", sum)
			dir := filepath.Join(t.TempDir(), "install with spaces", "bin")
			log := filepath.Join(t.TempDir(), "calls")
			kept := filepath.Join(filepath.Dir(dir), "my-result.txt")
			if err := os.MkdirAll(filepath.Dir(kept), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(kept, []byte("keep this result"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("UGT_FAKE_SETUP", state)
			launch := func(file, args string) error {
				cmd := exec.Command(file)
				// NSIS /D consumes the remaining command line, without quotes.
				cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `"` + file + `" ` + args}
				_, err := run(t, cmd, url, dir, log)
				return err
			}
			err := launch(installer, "/S /D="+dir)
			code := 0
			if err != nil {
				if e, ok := err.(*exec.ExitError); ok {
					code = e.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			want := map[string]int{"ready": 0, "restart": 3010, "failed": 1, "bad-digest": 1}[state]
			if code != want {
				t.Fatalf("setup exit %d, want %d", code, want)
			}
			if state == "bad-digest" {
				if calls, _ := os.ReadFile(log); len(calls) != 0 {
					t.Fatalf("executed an unverified helper: %s", calls)
				}
				return
			}
			for _, name := range []string{"ugt.exe", "Setup.exe", "Uninstall.exe"} {
				if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			k, err := registry.OpenKey(registry.CURRENT_USER, packageKey, registry.READ)
			if err != nil {
				t.Fatal("no Windows removal entry: ", err)
			}
			at, _, err := k.GetStringValue("InstallLocation")
			k.Close()
			if err != nil || at != dir {
				t.Fatalf("registered %q: %v", at, err)
			}
			if state == "restart" {
				t.Setenv("UGT_FAKE_SETUP", "ready")
				if err := launch(filepath.Join(dir, "Setup.exe"), "/S /D="+dir); err != nil {
					t.Fatal("setup did not resume from its installed shortcut target: ", err)
				}
			}
			if err := launch(filepath.Join(dir, "Uninstall.exe"), "/S _?="+dir); err != nil {
				t.Fatal("uninstall: ", err)
			}
			calls, _ := os.ReadFile(log)
			if !strings.Contains(string(calls), "setup --yes --json") || !strings.Contains(string(calls), "uninstall --yes") {
				t.Fatalf("did not use the helper's setup/removal owners: %s", calls)
			}
			if _, err := os.Stat(filepath.Join(dir, "ugt.exe")); !os.IsNotExist(err) {
				t.Fatalf("helper remained after removal: %v", err)
			}
			if got, _ := os.ReadFile(kept); string(got) != "keep this result" {
				t.Fatal("the package removed a result")
			}
			if k, err := registry.OpenKey(registry.CURRENT_USER, packageKey, registry.READ); err == nil {
				k.Close()
				t.Fatal("the Windows removal entry remained")
			}
		})
	}
}
