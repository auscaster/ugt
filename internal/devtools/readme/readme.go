// Package readme keeps documentation true to its sources: the skill's
// pitfalls table to the diagnosis rules, and the README's featured recipes to
// the prebuilt list. Nothing here reads a recipe: a recipe merge never changes
// a tracked file outside its own folder. The prebuilt list is the catalogue's
// one maintainer-kept file, and only it is read.
package readme

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/auscaster/ugt/internal/domain/diagnose"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

// featuredLink is a README link to a recipe's page.
var featuredLink = regexp.MustCompile(`https://usegithubthing\.com/recipes/([a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*/[a-z0-9]+(?:-[a-z0-9]+)*)\)`)

// Featured lists the recipes the README links, in order: its showcase, every
// one of which must be prebuilt.
func Featured(readme string) []string {
	var ids []string
	for _, m := range featuredLink.FindAllStringSubmatch(readme, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

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

// Bases is the skill's table of the official bases recipes start from, made
// from the list `ugt recipe new` drafts from.
func Bases() string {
	var b strings.Builder
	b.WriteString("\n| Base | For |\n| --- | --- |\n")
	for _, base := range recipe.Bases {
		fmt.Fprintf(&b, "| `%s` | %s |\n", base.Ref, base.For)
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
