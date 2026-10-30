// Package localdir implements the Catalogue port over a folder of recipes laid
// out as <owner>/<repo>/<task>/: the local catalogue of recipes made on this
// machine, or a checkout's recipes/ folder standing in for the published
// catalogue in development and CI.
package localdir

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

// Catalogue is a folder of recipes.
type Catalogue struct {
	source catalogue.Source
	fsys   fs.FS
}

var _ app.Catalogue = (*Catalogue)(nil)

// New returns the catalogue in fsys, naming its entries' source.
func New(source catalogue.Source, fsys fs.FS) *Catalogue {
	return &Catalogue{source: source, fsys: fsys}
}

func (c *Catalogue) Source() catalogue.Source { return c.source }

// Project lists the recipe folders under <owner>/<repo>/; a folder that does
// not exist holds none.
func (c *Catalogue) Project(_ context.Context, owner, repo string) ([]catalogue.Entry, error) {
	dir := strings.ToLower(owner + "/" + repo)
	tasks, err := fs.ReadDir(c.fsys, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []catalogue.Entry
	for _, t := range tasks {
		if t.IsDir() {
			if e, ok := c.entry(path.Join(dir, t.Name())); ok {
				out = append(out, e)
			}
		}
	}
	return out, nil
}

// Search lists the recipe folders whose ID holds every word.
func (c *Catalogue) Search(_ context.Context, words []string) ([]catalogue.Entry, error) {
	dirs, err := recipe.Find(c.fsys)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []catalogue.Entry
	for _, dir := range dirs {
		match := len(words) > 0
		for _, w := range words {
			match = match && strings.Contains(dir, strings.ToLower(w))
		}
		if !match {
			continue
		}
		if e, ok := c.entry(dir); ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// entry describes the recipe folder dir, when it holds one.
func (c *Catalogue) entry(dir string) (catalogue.Entry, bool) {
	if _, err := fs.Stat(c.fsys, path.Join(dir, recipe.RecordFile)); err != nil {
		return catalogue.Entry{}, false
	}
	sub, err := fs.Sub(c.fsys, dir)
	if err != nil {
		return catalogue.Entry{}, false
	}
	return catalogue.FromFolder(sub, dir, c.source), true
}

func (c *Catalogue) Open(_ context.Context, id string) (fs.FS, error) {
	if _, err := fs.Stat(c.fsys, path.Join(id, recipe.RecordFile)); err != nil {
		return nil, fmt.Errorf("%s in the %s catalogue: %w", id, c.source, app.ErrNotFound)
	}
	return fs.Sub(c.fsys, id)
}
