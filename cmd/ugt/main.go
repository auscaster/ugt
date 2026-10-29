// Command ugt is the Use the GitHub Thing helper.
//
// Only recipe validation exists so far; the helper's runtime commands are
// specified in the project plan and are not built yet.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/auscaster/ugt/internal/recipe"
)

const usage = `usage: ugt recipe validate RECIPE_DIR...

Validates recipe folders: recipe.json against the published schema and
compose.yaml against the recipe rules. A path to a recipe.json is read as its
folder. Exit status: 0 all valid, 1 one or more invalid, 2 usage error.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 3 || args[0] != "recipe" || args[1] != "validate" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	status := 0
	for _, target := range args[2:] {
		dir := target
		if filepath.Base(target) == "recipe.json" {
			dir = filepath.Dir(target)
		}
		if err := recipe.ValidateDir(dir); err != nil {
			fmt.Fprintf(stdout, "invalid  %s\n%v\n", target, err)
			status = 1
			continue
		}
		fmt.Fprintf(stdout, "ok       %s\n", target)
	}
	return status
}
