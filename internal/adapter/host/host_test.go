package host

import (
	"os"
	"path/filepath"
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
	info, err := os.Stat(filepath.Join(dst, "check", "run.sh"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("copy lost the executable bit: %v %v", info.Mode(), err)
	}
	if err := f.RemoveAll(dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("RemoveAll left the folder")
	}
}
