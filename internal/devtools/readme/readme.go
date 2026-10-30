// Package readme keeps generated documentation true to its sources: the
// skill's pitfalls table to the diagnosis rules. Nothing here is generated
// from or checked against the catalogue: a recipe merge never changes a
// tracked file outside its own folder, and product checks never read it.
package readme

import (
	"fmt"
	"strings"

	"github.com/auscaster/ugt/internal/domain/diagnose"
)

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
