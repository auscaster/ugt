package recipe

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
)

// Revision identifies a recipe's content: the first 12 hex characters of a
// SHA-256 over every file's slash path and the SHA-256 of its bytes, in path
// order. It is computed by tooling and never written by hand.
func Revision(fsys fs.FS) (string, error) {
	return digest(fsys, func(string) bool { return true })
}

// RunRevision identifies what a recipe runs: its revision over every file but
// recipe.json. Verification proves a run revision and its images are built
// from it, so a change to recipe.json alone, which says what the recipe is,
// keeps them.
func RunRevision(fsys fs.FS) (string, error) {
	return digest(fsys, func(p string) bool { return p != RecordFile })
}

func digest(fsys fs.FS, include func(path string) bool) (string, error) {
	var paths []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && include(p) {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(h, "%s\x00%x\n", p, sum)
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}
