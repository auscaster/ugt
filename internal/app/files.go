package app

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// recipeFileLimit bounds one recipe file written or read through a surface:
// recipes hold configuration and small scripts, never data.
const recipeFileLimit = 1 << 20

// ReadRecipe returns a recipe folder's text files by slash path, so an agent
// with no file access of its own (in a desktop chat app, say) can read what
// it is editing. Binary and oversized files are listed with no content.
func (a *App) ReadRecipe(dir string) (map[string]string, error) {
	out := map[string]string{}
	fsys := a.Disk.FS(dir)
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > recipeFileLimit {
			out[p] = ""
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) {
			data = nil
		}
		out[p] = string(data)
		return nil
	})
	return out, err
}

// WriteRecipe writes files into a recipe folder, by slash path relative to
// it, for agents with no file access of their own. Paths never leave the
// folder.
func (a *App) WriteRecipe(dir string, files map[string]string) ([]string, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		clean := path.Clean(strings.ReplaceAll(name, `\`, "/"))
		if clean == "." || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || filepath.VolumeName(name) != "" {
			return nil, fmt.Errorf("%q is not a path inside the recipe folder", name)
		}
		if len(files[name]) > recipeFileLimit {
			return nil, fmt.Errorf("%s is over %d MB; recipes hold configuration and scripts, not data", name, recipeFileLimit>>20)
		}
		if err := a.Disk.WriteFile(filepath.Join(dir, filepath.FromSlash(clean)), []byte(files[name])); err != nil {
			return nil, err
		}
	}
	return names, nil
}
