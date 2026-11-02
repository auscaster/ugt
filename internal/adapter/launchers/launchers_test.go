package launchers

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLaunchers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a Mac's launcher is an executable file, which Windows file systems do not have")
	}
	home := t.TempDir()
	f := New("darwin", home, "/Applications/UGT it's.app/ugt", func(name string) string {
		if name == "PATH" {
			return "/usr/bin:" + filepath.Join(home, ".local", "bin")
		}
		return ""
	})
	file, err := f.Install("tool")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	if info, _ := os.Stat(file); info.Mode().Perm()&0o100 == 0 || !strings.Contains(string(data), `exec '/Applications/UGT it'\''s.app/ugt' command tool "$@"`) {
		t.Errorf("launcher %s:\n%s", info.Mode(), data)
	}
	if on, _ := f.OnPath(); !on {
		t.Error("the folder is on PATH")
	}

	// a file UGT did not write is never replaced or removed
	mine := filepath.Join(f.Dir, "jq")
	os.WriteFile(mine, []byte("#!/bin/sh\necho mine\n"), 0o755)
	if _, err := f.Install("jq"); err == nil || !strings.Contains(err.Error(), "did not write") {
		t.Errorf("replaced the user's own jq: %v", err)
	}
	if err := f.Remove("jq"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mine); err != nil {
		t.Error("removed the user's own jq")
	}

	if err := f.Remove("tool"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("the launcher is still there")
	}
	if err := f.Remove("tool"); err != nil {
		t.Errorf("removing it again: %v", err)
	}
}
