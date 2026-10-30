package app

import "github.com/auscaster/ugt/internal/domain/recipe"

// Validate checks a recipe folder against the format and the Compose rules.
func (a *App) Validate(dir string) error { return recipe.ValidateDir(dir) }
