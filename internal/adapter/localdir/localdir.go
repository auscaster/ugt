// Package localdir implements the Catalogue port over a folder of recipes laid
// out as <owner>/<repo>/<task>/: the local catalogue of recipes made on this
// machine, the catalogue built into ugt, or a checkout's recipes/ folder.
package localdir

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"

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

// Entries lists every recipe folder; a folder that does not exist yet holds none.
func (c *Catalogue) Entries(context.Context) ([]catalogue.Entry, error) {
	dirs, err := recipe.Find(c.fsys)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []catalogue.Entry
	for _, dir := range dirs {
		sub, err := fs.Sub(c.fsys, dir)
		if err != nil {
			return nil, err
		}
		out = append(out, catalogue.FromFolder(sub, dir, c.source))
	}
	return out, nil
}

func (c *Catalogue) Open(_ context.Context, id string) (fs.FS, error) {
	if _, err := fs.Stat(c.fsys, path.Join(id, recipe.RecordFile)); err != nil {
		return nil, fmt.Errorf("%s in the %s catalogue: %w", id, c.source, app.ErrNotFound)
	}
	return fs.Sub(c.fsys, id)
}
