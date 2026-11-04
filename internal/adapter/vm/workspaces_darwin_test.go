package vm

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Run the guest's unlink command against real paths. A directory supplied
// by a recipe may contain links; no unlink may pass through one.
func TestWorkspaceRemoval(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "work spaces")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{root, outside} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(outside, "keep")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{root, outside, target} {
		if _, err := workspaceRemoval(root, p, true); err == nil {
			t.Fatalf("admitted %s", p)
		}
	}
	link := filepath.Join(root, "an 'outside' link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	remove := func(p string, tree bool) error {
		cmd, err := workspaceRemoval(root, p, tree)
		if err != nil {
			return err
		}
		return exec.Command(cmd.Name, cmd.Args...).Run()
	}
	if err := remove(filepath.Join(link, "keep"), true); err == nil {
		t.Fatal("accepted a symbolic parent")
	}
	if err := remove(link, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("link remained: %v", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "keep" {
		t.Fatalf("outside file: %q %v", data, err)
	}
	if err := remove(filepath.Join(root, "not created", "file"), false); err != nil {
		t.Fatal(err)
	}
}
