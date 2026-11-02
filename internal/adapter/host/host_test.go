package host

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFilesCopyAndList(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(src, "check"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"compose.yaml": "services: {}\n", "check/run.sh": "#!/bin/sh\ntrue\n"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dst := filepath.Join(root, "dst")
	f := Files{}
	if err := f.CopyIn(src, dst); err != nil {
		t.Fatal(err)
	}
	list, err := f.List(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Path != filepath.ToSlash(filepath.Join(dst, "check/run.sh")) || list[1].Size != 13 {
		t.Fatalf("list %+v", list)
	}
	// Windows has no executable bit to keep
	if info, err := os.Stat(filepath.Join(dst, "check", "run.sh")); runtime.GOOS != "windows" && (err != nil || info.Mode().Perm()&0o100 == 0) {
		t.Errorf("copy lost the executable bit: %v %v", info.Mode(), err)
	}
	if err := f.RemoveAll(dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("RemoveAll left the folder")
	}
}

// What a recipe writes reaches the user's disk as files only: a link in its
// outputs would copy whatever on this computer it points at.
func TestCopyOutLeavesLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links need privileges on Windows")
	}
	root := t.TempDir()
	secret := filepath.Join(root, "secret")
	if err := os.WriteFile(secret, []byte("the user's own\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outputs := filepath.Join(root, "outputs")
	if err := os.MkdirAll(filepath.Join(outputs, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputs, "talk.srt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{"stolen", "sub/stolen"} {
		if err := os.Symlink(secret, filepath.Join(outputs, link)); err != nil {
			t.Fatal(err)
		}
	}
	results := filepath.Join(root, "results")
	f := Files{}
	if err := f.CopyOut(outputs, results); err != nil {
		t.Fatal(err)
	}
	if err := f.CopyOut(filepath.Join(outputs, "stolen"), filepath.Join(root, "one")); err != nil {
		t.Fatal(err)
	}
	list, err := f.List(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, fi := range list {
		got = append(got, fi.Path)
	}
	want := []string{filepath.ToSlash(filepath.Join(outputs, "talk.srt")), filepath.ToSlash(filepath.Join(results, "talk.srt")), filepath.ToSlash(secret)}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("files\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
