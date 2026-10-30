package recipes

import (
	"testing"

	"github.com/auscaster/ugt/internal/domain/recipe"
)

// What ugt carries is the catalogue as admitted: every recipe valid, filed
// under its ID and verified.
func TestBuiltIn(t *testing.T) {
	dirs, err := recipe.Find(FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) < 4 {
		t.Fatalf("only %d recipes embedded: %v", len(dirs), dirs)
	}
	problems, err := recipe.CheckCatalogue(FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}
