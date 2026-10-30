package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/auscaster/ugt/internal/domain/admission"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

// Validate checks a recipe folder against the format and the Compose rules.
func (a *App) Validate(dir string) error { return recipe.Validate(a.Disk.FS(dir)) }

// Admit reads a recipe folder in a catalogue checkout, at
// .../recipes/<owner>/<repo>/<task>, for admission (spec/admission/v0): valid
// and filed under its ID, with what the catalogue service stores of it.
func (a *App) Admit(dir string) (admission.Record, error) {
	clean := filepath.ToSlash(filepath.Clean(dir))
	parts := strings.Split(clean, "/")
	if len(parts) < 3 {
		return admission.Record{}, fmt.Errorf("%s is not a catalogue folder: recipes/<owner>/<repo>/<task>", dir)
	}
	return admission.Read(a.Disk.FS(dir), strings.Join(parts[len(parts)-3:], "/")), nil
}
