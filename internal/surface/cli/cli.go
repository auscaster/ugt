// Package cli is the ugt command line: a thin surface over the app's use
// cases. Every command prints for people by default and takes --json for
// agents and CI.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/report"
)

// Exit statuses.
const (
	ExitOK     = 0
	ExitFailed = 1 // the work ran and did not succeed
	ExitUsage  = 2
	ExitBroken = 3 // the work could not run: no engine, unreadable recipe
)

// CLI runs ugt commands.
type CLI struct {
	App     *app.App
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
}

type command struct {
	usage string
	run   func(c *CLI, ctx context.Context, args []string) int
}

// commands is built on demand: its entries read it back for their usage.
func commands() map[string]command {
	return map[string]command{
		"recipe validate": {"RECIPE_DIR...", (*CLI).validate},
		"recipe verify":   {"[--pull [SUFFIX=]REF]... [--json] RECIPE_DIR", (*CLI).verify},
		"version":         {"", (*CLI).version},
	}
}

// Run runs one command line and returns its exit status.
func (c *CLI) Run(ctx context.Context, args []string) int {
	for n := 2; n >= 1; n-- {
		if len(args) >= n {
			if cmd, ok := commands()[strings.Join(args[:n], " ")]; ok {
				return cmd.run(c, ctx, args[n:])
			}
		}
	}
	c.usage()
	return ExitUsage
}

func (c *CLI) usage() {
	table := commands()
	names := make([]string, 0, len(table))
	for name := range table {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintln(c.Stderr, "usage:")
	for _, name := range names {
		fmt.Fprintf(c.Stderr, "  ugt %s %s\n", name, table[name].usage)
	}
}

func (c *CLI) version(_ context.Context, _ []string) int {
	fmt.Fprintf(c.Stdout, "ugt %s\n", c.Version)
	return ExitOK
}

// validate: ugt recipe validate RECIPE_DIR...; a recipe.json path is read as
// its folder.
func (c *CLI) validate(_ context.Context, args []string) int {
	if len(args) == 0 {
		c.usage()
		return ExitUsage
	}
	status := ExitOK
	for _, target := range args {
		dir := target
		if filepath.Base(target) == "recipe.json" {
			dir = filepath.Dir(target)
		}
		if err := c.App.Validate(dir); err != nil {
			fmt.Fprintf(c.Stdout, "invalid  %s\n%v\n", target, err)
			status = ExitFailed
			continue
		}
		fmt.Fprintf(c.Stdout, "ok       %s\n", target)
	}
	return status
}

// pulls collects --pull [SUFFIX=]REF flags.
type pulls map[string]string

func (p pulls) String() string { return fmt.Sprint(map[string]string(p)) }
func (p pulls) Set(v string) error {
	suffix, ref, ok := strings.Cut(v, "=")
	if !ok {
		suffix, ref = "", v
	}
	if ref == "" {
		return errors.New("empty image reference")
	}
	if suffix != "" && !strings.HasPrefix(suffix, "-") {
		return fmt.Errorf("suffix %q must start with '-', as in -api=REF", suffix)
	}
	p[suffix] = ref
	return nil
}

// verify: ugt recipe verify [--pull [SUFFIX=]REF]... [--json] RECIPE_DIR
func (c *CLI) verify(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt recipe verify", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	images := pulls{}
	fs.Var(images, "pull", "use an image already built instead of building: REF for $UGT_IMAGE, or -SUFFIX=REF")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		if err == nil {
			fmt.Fprintln(c.Stderr, "usage: ugt recipe verify "+commands()["recipe verify"].usage)
		}
		return ExitUsage
	}
	opts := app.VerifyOptions{Images: images}
	if !*asJSON {
		opts.Progress = func(step, message string) { fmt.Fprintf(c.Stdout, "%-13s %s\n", step, message) }
	}
	rep, err := c.App.Verify(ctx, fs.Arg(0), opts)
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitBroken
	}
	if *asJSON {
		enc := json.NewEncoder(c.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return ExitBroken
		}
	} else {
		c.printReport(rep)
	}
	if !rep.Passed {
		return ExitFailed
	}
	return ExitOK
}

func (c *CLI) printReport(rep report.Report) {
	w := c.Stdout
	fmt.Fprintln(w)
	if step, failed := rep.FailedStep(); failed {
		fmt.Fprintf(w, "FAILED at %s (exit %d) after %.0fs\n", step.Name, step.Exit, rep.Seconds)
		for _, f := range step.Findings {
			fmt.Fprintf(w, "\n  Why: %s\n  Fix: %s\n", f.Why, f.Fix)
			if f.Line != "" {
				fmt.Fprintf(w, "  Seen: %s\n", f.Line)
			}
		}
		if len(step.Findings) == 0 {
			fmt.Fprintf(w, "\nLast output:\n%s\n", indent(step.Log))
		}
	} else if weak := rep.WeakChecks(); len(weak) > 0 {
		fmt.Fprintf(w, "FAILED the check-strength test after %.0fs: a check accepted broken outputs\n", rep.Seconds)
		for _, p := range weak {
			fmt.Fprintf(w, "  %s passed with %s outputs\n", p.Check, p.Mutation)
		}
		fmt.Fprintln(w, "\n  Fix: make the check fail unless the output really has the right structure and content.")
	} else {
		fmt.Fprintf(w, "PASSED %s at revision %s on linux/%s in %.0fs; every check rejected broken outputs\n",
			rep.Recipe, rep.Revision, rep.Arch, rep.Seconds)
	}
	fmt.Fprintf(w, "Workspace: %s\n", rep.Workspace)
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}
