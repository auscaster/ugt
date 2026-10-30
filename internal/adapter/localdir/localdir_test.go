package localdir

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

func TestCatalogue(t *testing.T) {
	c, ctx := New(catalogue.BuiltIn, os.DirFS("../../../recipes")), context.Background()
	entries, err := c.Entries(ctx)
	if err != nil || len(entries) < 4 {
		t.Fatalf("entries %v, %v", entries, err)
	}
	for _, e := range entries {
		if e.Problem != "" || e.Source != catalogue.BuiltIn {
			t.Errorf("entry %+v", e)
		}
	}
	fsys, err := c.Open(ctx, "openai/whisper/subtitles")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recipe.Load(fsys); err != nil {
		t.Errorf("opened folder: %v", err)
	}
	if _, err := c.Open(ctx, "no/such/recipe"); !errors.Is(err, app.ErrNotFound) {
		t.Errorf("missing recipe: %v", err)
	}
}

func TestMissingFolderIsEmpty(t *testing.T) {
	entries, err := New(catalogue.Local, os.DirFS(t.TempDir()+"/not-yet")).Entries(context.Background())
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries %v, %v", entries, err)
	}
	_ = fs.ErrNotExist
}
