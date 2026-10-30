package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/auscaster/ugt/internal/domain/recipe"
)

// Validate checks a recipe folder against the format and the Compose rules.
func (a *App) Validate(dir string) error { return recipe.Validate(a.Disk.FS(dir)) }

// Admit checks a recipe folder in a catalogue checkout, at
// .../recipes/<owner>/<repo>/<task>, for admission: valid, filed under its ID,
// and recording where it was verified.
func (a *App) Admit(dir string) error {
	clean := filepath.ToSlash(filepath.Clean(dir))
	parts := strings.Split(clean, "/")
	if len(parts) < 3 {
		return fmt.Errorf("%s is not a catalogue folder: recipes/<owner>/<repo>/<task>", dir)
	}
	if problems := recipe.Admit(a.Disk.FS(dir), strings.Join(parts[len(parts)-3:], "/")); len(problems) > 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	return nil
}
