// Command render writes a release's package manager manifests from its signed
// list in dir (release.json and release.json.sig), checked with UGT's release
// key:
//
//	go run ./internal/devtools/channels/render -dir release -out channels
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/auscaster/ugt/internal/devtools/channels"
	"github.com/auscaster/ugt/internal/domain/release"
)

func main() {
	dir := flag.String("dir", "", "the folder holding the release's signed list")
	out := flag.String("out", "", "the folder to write the manifests to")
	flag.Parse()
	if *dir == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := render(*dir, *out); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}

func render(dir, out string) error {
	data, err := os.ReadFile(filepath.Join(dir, release.ListFile))
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(filepath.Join(dir, release.SignatureFile))
	if err != nil {
		return err
	}
	list, err := release.Parse(data, sig)
	if err != nil {
		return err
	}
	files, err := channels.Render(list)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		path := filepath.Join(out, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(files[p]), 0o644); err != nil {
			return err
		}
		fmt.Println(path)
	}
	return nil
}
