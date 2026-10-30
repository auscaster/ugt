// Package readme keeps generated documentation true to its sources: the
// skill's pitfalls table to the diagnosis rules, and the README's links to the
// recipe folders it features. Nothing here is generated from the catalogue:
// a recipe merge never changes a tracked file outside its own folder.
package readme

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/auscaster/ugt/internal/domain/diagnose"
)

// A link to a recipe folder: recipes/<owner>/<repo>/<task>.
var recipeLink = regexp.MustCompile(`\]\((recipes/[^/)#\s]+/[^/)#\s]+/[^/)#\s]+)/?\)`)

// Pitfalls is the skill's table of what verification diagnoses, made from
// the same rules the reports use, so the skill and the helper teach one thing.
func Pitfalls() string {
	var b strings.Builder
	b.WriteString("\n| Pitfall | What went wrong | The fix |\n| --- | --- | --- |\n")
	for _, r := range diagnose.Rules {
		cell := func(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", r.ID, cell(r.Cause), cell(r.Fix))
	}
	return b.String()
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
