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
		"recipe new":      {"[--from FILE|URL|PATH] [--blank] [--kind job|service] [--what TEXT] [--dir DIR] [--json] OWNER/REPO TASK", (*CLI).newRecipe},
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

// newRecipe: ugt recipe new [flags] OWNER/REPO TASK
func (c *CLI) newRecipe(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt recipe new", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	var opts app.NewOptions
	fs.StringVar(&opts.From, "from", "", "adopt this Compose file: a path in the project, a github.com link to a file, or a file on this machine")
	fs.BoolVar(&opts.Blank, "blank", false, "start blank even when the project ships a Compose file")
	fs.StringVar(&opts.Kind, "kind", "", "job (runs to completion) or service (keeps running); inferred when not given")
	fs.StringVar(&opts.What, "what", "", "the task in plain words, as \"Turn a recording into an .srt subtitle file\"")
	fs.StringVar(&opts.Dir, "dir", "", "where to write the recipe; the local catalogue when not given")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := parseInterspersed(fs, args); err != nil || fs.NArg() != 2 {
		if err == nil {
			fmt.Fprintln(c.Stderr, "usage: ugt recipe new "+commands()["recipe new"].usage)
		}
		return ExitUsage
	}
	opts.Project, opts.Task = fs.Arg(0), fs.Arg(1)
	res, err := c.App.New(ctx, opts)
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitBroken
	}
	if *asJSON {
		return c.printJSON(res)
	}
	w := c.Stdout
	fmt.Fprintf(w, "Started %s (%s) at commit %s\n  in %s\n", res.Recipe, res.Kind, res.Commit[:12], res.Dir)
	if res.Adopted != "" {
		fmt.Fprintf(w, "  adopted from %s\n", res.Adopted)
	}
	fmt.Fprintf(w, "Files: %s\n", strings.Join(res.Files, ", "))
	if len(res.Changes) > 0 {
		fmt.Fprintln(w, "\nChanged from the original:")
		for _, ch := range res.Changes {
			fmt.Fprintf(w, "  - %s\n", ch)
		}
	}
	if len(res.Problems) > 0 {
		fmt.Fprintln(w, "\nProblems to resolve:")
		for _, p := range res.Problems {
			fmt.Fprintf(w, "  - %s\n", p)
		}
	}
	fmt.Fprintln(w, "\nNext:")
	for i, step := range res.Next {
		fmt.Fprintf(w, "  %d. %s\n", i+1, step)
	}
	return ExitOK
}

// parseInterspersed parses flags wherever they appear among the arguments,
// so "ugt recipe new a/b task --json" works as well as flags first.
func parseInterspersed(fs *flag.FlagSet, args []string) error {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	return fs.Parse(append([]string{"--"}, positional...))
}

func (c *CLI) printJSON(v any) int {
	enc := json.NewEncoder(c.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return ExitBroken
	}
	return ExitOK
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
		if status := c.printJSON(rep); status != ExitOK {
			return status
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
