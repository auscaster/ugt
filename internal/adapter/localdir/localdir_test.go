package localdir

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

// fixture lays the spec's example job out as a catalogue: example/tool/markdown.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, "example", "tool", "markdown"), os.DirFS("../../../spec/recipe/v0/examples/valid/example-job")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCatalogue(t *testing.T) {
	c, ctx := New(catalogue.Published, os.DirFS(fixture(t))), context.Background()
	entries, err := c.Project(ctx, "Example", "tool")
	if err != nil || len(entries) != 1 || entries[0].Recipe != "example/tool/markdown" || entries[0].Problem != "" || entries[0].Source != catalogue.Published {
		t.Fatalf("project %+v, %v", entries, err)
	}
	if none, err := c.Project(ctx, "someone", "else"); err != nil || len(none) != 0 {
		t.Errorf("a project with no recipes: %v, %v", none, err)
	}
	if found, err := c.Search(ctx, []string{"markdown"}); err != nil || len(found) != 1 {
		t.Errorf("search %v, %v", found, err)
	}
	fsys, err := c.Open(ctx, "example/tool/markdown")
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
	c := New(catalogue.Local, os.DirFS(filepath.Join(t.TempDir(), "not-yet")))
	if entries, err := c.Project(context.Background(), "a", "b"); err != nil || len(entries) != 0 {
		t.Fatalf("project %v, %v", entries, err)
	}
	if entries, err := c.Search(context.Background(), []string{"a"}); err != nil || len(entries) != 0 {
		t.Fatalf("search %v, %v", entries, err)
	}
}
