package recipe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Revision identifies a recipe's content: the first 12 hex characters of a
// SHA-256 over every file's slash path and the SHA-256 of its bytes, in path
// order. recipe.json is canonicalised without its verified block, so
// recording a verification never changes what was verified. It is computed by
// tooling and never written by hand.
func Revision(dir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return "", err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if rel == RecordFile {
			if data, err = withoutVerified(data); err != nil {
				return "", err
			}
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(h, "%s\x00%x\n", rel, sum)
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// withoutVerified re-encodes recipe.json without "verified", with sorted keys.
func withoutVerified(data []byte) ([]byte, error) {
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("%s: %w", RecordFile, err)
	}
	delete(record, "verified")
	return json.Marshal(record)
}
