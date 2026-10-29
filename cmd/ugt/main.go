// Command ugt is the Use the GitHub Thing helper.
//
// Only recipe validation exists so far; the helper's runtime commands are
// specified in the project plan and are not built yet.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/auscaster/ugt/internal/recipe"
)

const usage = `usage: ugt recipe validate FILE...

Validates UGT recipe records against the published recipe schema.
Exit status: 0 all valid, 1 one or more invalid, 2 usage error.
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
	for _, path := range args[2:] {
		data, err := os.ReadFile(path)
		if err == nil {
			err = recipe.Validate(data)
		}
		if err != nil {
			fmt.Fprintf(stdout, "invalid  %s\n%v\n", path, err)
			status = 1
			continue
		}
		fmt.Fprintf(stdout, "ok       %s\n", path)
	}
	return status
}
