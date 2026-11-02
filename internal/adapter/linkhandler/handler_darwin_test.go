package linkhandler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// UGT.app takes ugt:// links and passes them to ugt, and is made again only
// when the command changes; unregistering removes it.
func TestAppTakesUgtLinks(t *testing.T) {
	home := t.TempDir()
	var calls []string
	h := &Handler{home: home, launchServ: func(flag, app string) error {
		calls = append(calls, flag+" "+app)
		return nil
	}}
	changed, err := h.Register([]string{"/Users/me/.local/bin/ugt", "open"})
	if err != nil || !changed {
		t.Fatalf("%v, %v", changed, err)
	}
	plist := filepath.Join(h.app(), "Contents", "Info.plist")
	if out, err := exec.Command("plutil", "-extract", "CFBundleURLTypes.0.CFBundleURLSchemes.0", "raw", plist).Output(); err != nil || strings.TrimSpace(string(out)) != "ugt" {
		t.Errorf("scheme %q, %v", out, err)
	}
	if out, err := exec.Command("codesign", "--verify", h.app()).CombinedOutput(); err != nil {
		t.Errorf("not signed: %s", out)
	}
	if again, err := h.Register([]string{"/Users/me/.local/bin/ugt", "open"}); err != nil || again {
		t.Errorf("the same command again: %v, %v", again, err)
	}
	if moved, err := h.Register([]string{"/opt/ugt", "open"}); err != nil || !moved {
		t.Errorf("a new command: %v, %v", moved, err)
	}
	if got, _ := os.ReadFile(h.marker()); string(got) != `'/opt/ugt' 'open'` {
		t.Errorf("passes links to %q", got)
	}
	if err := h.Unregister(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(h.app()); !os.IsNotExist(err) {
		t.Error("UGT.app stayed")
	}
	if want := "-f " + h.app() + ",-f " + h.app() + ",-u " + h.app(); strings.Join(calls, ",") != want {
		t.Errorf("LaunchServices: %v", calls)
	}
}

// macOS hands a ugt:// link to UGT.app, which hands it to the command. It
// registers an app with this Mac's LaunchServices for a moment, so it runs
// only when asked: UGT_LAUNCHSERVICES_TEST=1. The app is made outside the
// temporary folders, from which LaunchServices launches nothing.
func TestMacHandsLinksToTheCommand(t *testing.T) {
	if os.Getenv("UGT_LAUNCHSERVICES_TEST") == "" {
		t.Skip("set UGT_LAUNCHSERVICES_TEST=1 to register UGT.app with this Mac for a moment")
	}
	home, err := os.MkdirTemp("/Users/Shared", "ugt-links-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	got := filepath.Join(home, "link")
	h := New(home)
	// the command writes the link it is given to got
	if _, err := h.Register([]string{"/bin/sh", "-c", `printf %s "$1" > "$0"`, got}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Unregister() })
	if out, err := exec.Command("open", "ugt://run/acme/tool/report").CombinedOutput(); err != nil {
		t.Fatalf("open: %v %s", err, out)
	}
	for {
		if data, err := os.ReadFile(got); err == nil && len(data) > 0 {
			if string(data) != "ugt://run/acme/tool/report" {
				t.Errorf("the command was given %q", data)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
