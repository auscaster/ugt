// Package readme keeps the README's generated parts true to recipes/: the
// recipes badge and the catalogue line between their markers, and the recipe
// folders the README links to.
package readme

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/auscaster/ugt/internal/domain/recipe"
)

// A link to a recipe folder: recipes/<owner>/<repo>/<task>.
var recipeLink = regexp.MustCompile(`\]\((recipes/[^/)#\s]+/[^/)#\s]+/[^/)#\s]+)/?\)`)

// Catalogue counts the recipes under root/recipes and how many are verified.
type Catalogue struct {
	Recipes, Verified int
}

// Count reads the catalogue under root/recipes.
func Count(root string) (Catalogue, error) {
	dirs, err := recipe.Find(filepath.Join(root, "recipes"))
	if err != nil {
		return Catalogue{}, err
	}
	c := Catalogue{Recipes: len(dirs)}
	for _, dir := range dirs {
		data, err := os.ReadFile(filepath.Join(dir, "recipe.json"))
		if err != nil {
			return Catalogue{}, err
		}
		var r struct {
			Verified *struct{} `json:"verified"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return Catalogue{}, fmt.Errorf("%s: %w", dir, err)
		}
		if r.Verified != nil {
			c.Verified++
		}
	}
	return c, nil
}

// Blocks returns each generated README block by marker name.
func (c Catalogue) Blocks() map[string]string {
	proof := fmt.Sprintf("%d verified on a known sample", c.Verified)
	if c.Verified == c.Recipes {
		proof = "every one verified on a known sample"
	}
	return map[string]string{
		// inline, inside the badge row: a comment starting a line would split the row
		"recipes-badge": fmt.Sprintf("[![Recipes: %d verified](https://img.shields.io/badge/recipes-%d%%20verified-111?style=flat-square)](recipes)",
			c.Verified, c.Verified),
		"catalogue": fmt.Sprintf("\n**%d recipes** so far, %s. Browse them all in [`recipes/`](recipes).\n",
			c.Recipes, proof),
	}
}

// Replace returns readme with the text between <!-- name:start --> and
// <!-- name:end --> replaced by block, exactly.
func Replace(readme, name, block string) (string, error) {
	start, end := "<!-- "+name+":start -->", "<!-- "+name+":end -->"
	before, rest, ok := strings.Cut(readme, start)
	if !ok {
		return "", fmt.Errorf("README has no %s marker", start)
	}
	_, after, ok := strings.Cut(rest, end)
	if !ok {
		return "", fmt.Errorf("README has no %s marker", end)
	}
	return before + start + block + end + after, nil
}

// DeadRecipeLinks returns links into recipes/ that are not recipe folders,
// so a featured example can never point at a recipe that is gone.
func DeadRecipeLinks(readme, root string) []string {
	var dead []string
	for _, m := range recipeLink.FindAllStringSubmatch(readme, -1) {
		if _, err := os.Stat(filepath.Join(root, m[1], "recipe.json")); err != nil {
			dead = append(dead, m[1])
		}
	}
	return dead
}
