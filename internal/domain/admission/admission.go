// Package admission is what a verification run reports to the catalogue
// service about one recipe revision (spec/admission/v0): everything the
// service stores and serves, read here, once, so the service never parses a
// recipe or computes a revision.
package admission

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"

	"github.com/auscaster/ugt/internal/domain/access"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

// Record is one recipe folder, as admission reads it.
type Record struct {
	Recipe   string   `json:"recipe"` // owner/repo/task, from its folder
	Revision string   `json:"revision,omitempty"`
	Run      string   `json:"run,omitempty"`      // its run revision: what verification proves
	Problems []string `json:"problems,omitempty"` // empty when it can be admitted
	Entry    *Entry   `json:"entry,omitempty"`
	// Gated are the kinds of access it declares that a maintainer reviews
	// before it is verified.
	Gated []string `json:"gated,omitempty"`
	Files []File   `json:"files,omitempty"`
}

// Entry is what the catalogue says about a recipe.
type Entry struct {
	Task     string           `json:"task"`
	Kind     string           `json:"kind"`
	Repo     string           `json:"repo"`
	Commit   string           `json:"commit"`
	Licence  string           `json:"licence"`
	Settings []recipe.Setting `json:"settings,omitempty"`
	Access   []recipe.Access  `json:"access,omitempty"`
	Inputs   []recipe.File    `json:"inputs,omitempty"`
	Outputs  []recipe.File    `json:"outputs,omitempty"`
	Provides recipe.Provides  `json:"provides,omitzero"`
}

// File is one file of the folder, with the bytes the catalogue serves.
type File struct {
	Path    string `json:"path"`
	Mode    string `json:"mode"` // 100644, or 100755 for an executable
	SHA     string `json:"sha"`  // its Git blob hash
	Size    int64  `json:"size"`
	Content []byte `json:"content"`
}

// Read reads the recipe folder at the root of fsys, filed in the catalogue
// as id (owner/repo/task).
func Read(fsys fs.FS, id string) Record {
	rec := Record{Recipe: id, Problems: recipe.Admit(fsys, id)}
	f, err := recipe.Load(fsys)
	if err != nil {
		return rec
	}
	if rec.Revision, err = recipe.Revision(fsys); err != nil {
		rec.Problems = append(rec.Problems, err.Error())
		return rec
	}
	if rec.Run, err = recipe.RunRevision(fsys); err != nil {
		rec.Problems = append(rec.Problems, err.Error())
		return rec
	}
	r := f.Recipe
	rec.Entry = &Entry{Task: r.Task, Kind: r.Kind, Repo: r.Repo, Commit: r.Commit, Licence: r.Licence,
		Settings: r.Settings, Access: r.Access, Inputs: r.Inputs, Outputs: r.Outputs, Provides: r.Provides}
	for _, a := range r.Access {
		if access.Kind(a.Kind).Gated() {
			rec.Gated = append(rec.Gated, a.Kind)
		}
	}
	err = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a file", p) // Load refused it already
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		mode := "100644"
		if info.Mode()&0o111 != 0 {
			mode = "100755"
		}
		rec.Files = append(rec.Files, File{Path: p, Mode: mode, SHA: BlobSHA(data), Size: int64(len(data)), Content: data})
		return nil
	})
	if err != nil {
		rec.Problems = append(rec.Problems, fmt.Sprintf("read %s: %v", id, err))
	}
	sort.Slice(rec.Files, func(i, j int) bool { return rec.Files[i].Path < rec.Files[j].Path })
	return rec
}

// BlobSHA is Git's hash of a file's bytes, which names a recipe file in the
// catalogue.
func BlobSHA(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
