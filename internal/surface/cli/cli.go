// Package cli is the ugt command line: a thin surface over the app's use
// cases. Every command prints for people by default and takes --json for
// agents and CI.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/recipe"
	"github.com/auscaster/ugt/internal/domain/report"
	"golang.org/x/term"
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
	Stdin   io.Reader // where settings set reads a value; a terminal reads secrets without echo
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
	// MCP serves the MCP surface over stdio, for ugt mcp; the composition
	// root provides it.
	MCP func(ctx context.Context) error
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
		"recipe verify":   {"[--pull [SUFFIX=]REF]... [--from STEP] [--json] RECIPE_DIR", (*CLI).verify},
		"recipe exec":     {"RECIPE_DIR SERVICE [-- COMMAND [ARG]...]", (*CLI).exec},
		"version":         {"", (*CLI).version},
		"mcp":             {"", (*CLI).mcp},
		"setup":           {"[--yes] [--json]", (*CLI).setup},
		"doctor":          {"[--json]", (*CLI).doctor},
		"connect":         {"[--json] [APP]...", (*CLI).connect},
		"settings set":    {"RECIPE NAME   (reads the value from the terminal, or from a pipe)", (*CLI).settingsSet},
		"settings list":   {"[--json] RECIPE", (*CLI).settingsList},
		"settings forget": {"RECIPE NAME", (*CLI).settingsForget},
		"disconnect":      {"[--json] [APP]...", (*CLI).disconnect},
		"uninstall":       {"[--yes] [--json]", (*CLI).uninstall},
		"find":            {"[--json] OWNER/REPO|WORDS...", (*CLI).find},
		"run":             {"[--input FILE|NAME=FILE]... [--to DIR] [--set NAME=VALUE]... [--json] OWNER/REPO [TASK]", (*CLI).run},
		"list":            {"[--json]", (*CLI).list},
		"stop":            {"[--json] RECIPE", (*CLI).stop},
		"remove":          {"[--json] RECIPE", (*CLI).remove},
		"delete-data":     {"--yes [--json] RECIPE", (*CLI).deleteData},
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

// setup: ugt setup [--yes]. --yes agrees to steps that need admin approval
// or a restart, such as installing WSL.
func (c *CLI) setup(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt setup", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	yes := fs.Bool("yes", false, "agree to steps that need admin approval or a restart")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	opts := app.SetupOptions{AllowAdmin: *yes}
	if !*asJSON {
		opts.Progress = func(step, message string) { fmt.Fprintf(c.Stdout, "%-10s %s\n", step, message) }
	}
	res, err := c.App.Setup(ctx, opts)
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitBroken
	}
	if *asJSON {
		c.printJSON(res)
	} else {
		for _, d := range res.Done {
			fmt.Fprintf(c.Stdout, "Done: %s\n", d)
		}
		for _, n := range res.Next {
			fmt.Fprintf(c.Stdout, "Next: %s\n", n)
		}
		if res.Ready {
			c.suggestConnect(ctx)
			fmt.Fprintln(c.Stdout, "Ready. Ask your AI to \"Use the GitHub Thing\", or try ugt find <owner>/<repo>.")
		}
	}
	if !res.Ready {
		return ExitFailed
	}
	return ExitOK
}

// settingsSet: ugt settings set RECIPE NAME. The value is never an argument,
// so it stays out of shell history and process lists: it is typed at a
// prompt, without echo for a secret, or piped in.
func (c *CLI) settingsSet(ctx context.Context, args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(c.Stderr, "usage: ugt settings set "+commands()["settings set"].usage)
		return ExitUsage
	}
	s, err := c.App.SettingKind(ctx, args[0], args[1])
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitFailed
	}
	value, err := c.readValue(s)
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitFailed
	}
	if value == "" {
		fmt.Fprintln(c.Stderr, "ugt: nothing was entered; nothing changed")
		return ExitFailed
	}
	id, err := c.App.SetSetting(ctx, args[0], args[1], value)
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitFailed
	}
	where := "UGT's settings"
	if s.Kind == "secret" {
		where = "the system's secret store"
	}
	fmt.Fprintf(c.Stdout, "Set %s for %s, in %s.\n", s.Name, id, where)
	return ExitOK
}

// readValue reads one line: from a terminal with a prompt, hidden for a
// secret, or from whatever is piped in.
func (c *CLI) readValue(s recipe.Setting) (string, error) {
	if f, ok := c.Stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprintf(c.Stderr, "%s (%s)", s.Name, s.What)
		if s.How != "" {
			fmt.Fprintf(c.Stderr, "\n  %s", s.How)
		}
		fmt.Fprint(c.Stderr, "\n> ")
		if s.Kind == "secret" {
			b, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(c.Stderr)
			return strings.TrimSpace(string(b)), err
		}
	}
	line, err := bufio.NewReader(c.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (c *CLI) settingsList(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt settings list", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := parseInterspersed(fs, args); err != nil || fs.NArg() != 1 {
		return ExitUsage
	}
	id, states, err := c.App.Settings(ctx, fs.Arg(0))
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitFailed
	}
	if *asJSON {
		if states == nil {
			states = []app.SettingState{}
		}
		return c.printJSON(map[string]any{"recipe": id, "settings": states})
	}
	if len(states) == 0 {
		fmt.Fprintf(c.Stdout, "%s has no settings.\n", id)
	}
	for _, st := range states {
		state := "not set"
		switch {
		case st.Set && st.Kind == "secret":
			state = "set, in the system's secret store"
		case st.Set:
			state = "set to " + st.Value
		case st.Required:
			state = "not set, and required: ugt settings set " + id + " " + st.Name
		}
		fmt.Fprintf(c.Stdout, "%s (%s): %s\n  %s\n", st.Name, st.Kind, state, st.What)
	}
	return ExitOK
}

func (c *CLI) settingsForget(ctx context.Context, args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(c.Stderr, "usage: ugt settings forget "+commands()["settings forget"].usage)
		return ExitUsage
	}
	id, err := c.App.ForgetSetting(ctx, args[0], args[1])
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitFailed
	}
	fmt.Fprintf(c.Stdout, "Forgot %s for %s.\n", args[1], id)
	return ExitOK
}

// suggestConnect names the AI apps found here that UGT is not connected to.
func (c *CLI) suggestConnect(ctx context.Context) {
	if c.App.Agents == nil {
		return
	}
	found, err := c.App.Agents.Find(ctx)
	if err != nil {
		return
	}
	var titles []string
	for _, h := range found {
		if !h.Connected {
			titles = append(titles, h.Title)
		}
	}
	if n := len(titles); n > 1 {
		fmt.Fprintf(c.Stdout, "Next: connect %s and %s with ugt connect.\n", strings.Join(titles[:n-1], ", "), titles[n-1])
	} else if n == 1 {
		fmt.Fprintf(c.Stdout, "Next: connect %s with ugt connect.\n", titles[0])
	}
}

// connect: ugt connect [APP]... connects every AI app found when none is named.
func (c *CLI) connect(ctx context.Context, args []string) int {
	return c.connection(ctx, "connect", args, c.App.Connect)
}

func (c *CLI) disconnect(ctx context.Context, args []string) int {
	return c.connection(ctx, "disconnect", args, c.App.Disconnect)
}

func (c *CLI) connection(ctx context.Context, name string, args []string, do func(context.Context, []string) ([]app.Connection, error)) int {
	fs := flag.NewFlagSet("ugt "+name, flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := parseInterspersed(fs, args); err != nil {
		return ExitUsage
	}
	done, err := do(ctx, fs.Args())
	if *asJSON && err == nil {
		return c.printJSON(done)
	}
	for _, d := range done {
		fmt.Fprintf(c.Stdout, "%s: %s\n", d.App, d.Done)
	}
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitFailed
	}
	return ExitOK
}

// uninstall: ugt uninstall [--yes]. Without --yes it only says what it
// would remove; --yes is the consent, given by flag, never a prompt.
func (c *CLI) uninstall(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt uninstall", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	yes := fs.Bool("yes", false, "agree to removing everything UGT added, saved data included")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	res, err := c.App.Uninstall(ctx, *yes)
	if *asJSON {
		c.printJSON(res)
	} else {
		w := c.Stdout
		lead := "Removed:"
		if !res.Applied {
			lead = "ugt uninstall would:"
		}
		fmt.Fprintln(w, lead)
		for _, s := range res.Steps {
			fmt.Fprintf(w, "  - %s\n", s)
		}
		if len(res.Steps) == 0 {
			fmt.Fprintln(w, "  nothing: UGT has added nothing here")
		}
		fmt.Fprintln(w, "Kept:")
		for _, k := range res.Kept {
			fmt.Fprintf(w, "  - %s\n", k)
		}
		for _, f := range res.Failed {
			fmt.Fprintf(w, "Failed: %s\n", f)
		}
		if !res.Applied {
			fmt.Fprintln(w, "Saved data goes for good. Run ugt uninstall --yes to agree.")
		}
	}
	switch {
	case err != nil:
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitFailed
	case !res.Applied:
		return ExitUsage
	}
	return ExitOK
}

// doctor: ugt doctor
func (c *CLI) doctor(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt doctor", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	rep, err := c.App.Doctor(ctx)
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitBroken
	}
	if *asJSON {
		c.printJSON(rep)
	} else {
		for _, ch := range rep.Checks {
			mark := "ok  "
			switch {
			case !ch.OK:
				mark = "FIX "
			case ch.Warning:
				mark = "warn"
			}
			fmt.Fprintf(c.Stdout, "%s %-9s %s\n", mark, ch.Name, ch.Detail)
			if ch.Fix != "" {
				fmt.Fprintf(c.Stdout, "               %s\n", ch.Fix)
			}
		}
	}
	if !rep.OK {
		return ExitFailed
	}
	return ExitOK
}

// mcp: ugt mcp serves agents over stdio until the client disconnects.
func (c *CLI) mcp(ctx context.Context, args []string) int {
	if len(args) != 0 || c.MCP == nil {
		return ExitUsage
	}
	if err := c.MCP(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintf(c.Stderr, "ugt mcp: %v\n", err)
		return ExitBroken
	}
	return ExitOK
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

// exec: ugt recipe exec RECIPE_DIR SERVICE [-- COMMAND [ARG]...]. It exits
// with the command's own status.
func (c *CLI) exec(ctx context.Context, args []string) int {
	before, command := args, []string(nil)
	if i := slices.Index(args, "--"); i >= 0 {
		before, command = args[:i], args[i+1:]
	}
	if len(before) != 2 {
		fmt.Fprintln(c.Stderr, "usage: ugt recipe exec "+commands()["recipe exec"].usage)
		return ExitUsage
	}
	res, err := c.App.Exec(ctx, before[0], before[1], command, c.Stdout)
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitBroken
	}
	fmt.Fprintf(c.Stderr, "ugt: %s exited %d, in the workspace of the last verification (%s) with its image %s\n",
		res.Service, res.Exit, res.Workspace, res.Image)
	return res.Exit
}

// find: ugt find OWNER/REPO|WORDS...
func (c *CLI) find(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt find", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := parseInterspersed(fs, args); err != nil || fs.NArg() == 0 {
		if err == nil {
			fmt.Fprintln(c.Stderr, "usage: ugt find "+commands()["find"].usage)
		}
		return ExitUsage
	}
	res, err := c.App.Find(ctx, strings.Join(fs.Args(), " "))
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitBroken
	}
	if *asJSON {
		return c.printJSON(res)
	}
	for _, e := range res.Recipes {
		state := "verified"
		switch {
		case e.Problem != "":
			state = "does not validate yet"
		case e.Here:
			state = "verified on this machine"
		case !e.Verified:
			state = "not verified yet"
		}
		fmt.Fprintf(c.Stdout, "%-40s %s\n  %s (%s, %s)\n", e.Recipe, e.Kind, e.What, e.Source, state)
	}
	for _, n := range res.Next {
		fmt.Fprintln(c.Stdout, n)
	}
	return ExitOK
}

// settingValues collects --set NAME=VALUE flags.
type settingValues map[string]string

func (v settingValues) String() string { return fmt.Sprint(map[string]string(v)) }
func (v settingValues) Set(s string) error {
	name, value, ok := strings.Cut(s, "=")
	if !ok || name == "" {
		return errors.New("give NAME=VALUE")
	}
	v[name] = value
	return nil
}

// repeated collects a flag given several times.
type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, ",") }
func (r *repeated) Set(s string) error { *r = append(*r, s); return nil }

// run: ugt run [flags] OWNER/REPO [TASK]
func (c *CLI) run(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt run", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	var inputs repeated
	values := settingValues{}
	fs.Var(&inputs, "input", "an input file, as FILE or NAME=FILE; a bare name is looked for in the UGT folder")
	fs.Var(values, "set", "a value setting, as NAME=VALUE; secrets are never given here")
	to := fs.String("to", "", "where the results go; the recipe's folder in the UGT folder when not given")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := parseInterspersed(fs, args); err != nil || fs.NArg() < 1 || fs.NArg() > 2 {
		if err == nil {
			fmt.Fprintln(c.Stderr, "usage: ugt run "+commands()["run"].usage)
		}
		return ExitUsage
	}
	opts := app.RunOptions{Project: fs.Arg(0), Task: fs.Arg(1), Inputs: inputs, To: *to, Values: values}
	if !*asJSON {
		opts.Progress = func(step, message string) { fmt.Fprintf(c.Stdout, "%-13s %s\n", step, message) }
	}
	res, err := c.App.Run(ctx, opts)
	var none *catalogue.NoRecipe
	if errors.As(err, &none) {
		if *asJSON {
			c.printJSON(map[string]any{"recipe": nil, "error": err.Error(), "next": noRecipeNext(none)})
			return ExitFailed
		}
		fmt.Fprintf(c.Stdout, "%v.\n", err)
		for _, n := range noRecipeNext(none) {
			fmt.Fprintln(c.Stdout, n)
		}
		return ExitFailed
	}
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitBroken
	}
	if *asJSON {
		c.printJSON(res)
	} else {
		c.printRun(res)
	}
	if !res.Passed {
		return ExitFailed
	}
	return ExitOK
}

// noRecipeNext is the authoring loop, for a project with no recipe yet.
func noRecipeNext(none *catalogue.NoRecipe) []string {
	task := none.Task
	if task == "" {
		task = "<task>"
	}
	return []string{
		"That is normal: a recipe makes it faster, and you can make one.",
		fmt.Sprintf("  1. ugt recipe new %s %s", none.Project, task),
		"  2. write its services, then ugt recipe verify <folder> until it passes",
		fmt.Sprintf("  3. ugt run %s %s --input FILE", none.Project, strings.TrimPrefix(task, "<task>")),
	}
}

func (c *CLI) printRun(res app.RunResult) {
	w := c.Stdout
	fmt.Fprintln(w)
	for _, step := range res.Steps {
		if step.Exit != 0 {
			c.printFailure(step, res.Seconds)
			return
		}
	}
	if res.Kind == "service" {
		fmt.Fprintf(w, "Running %s; its check passed.\n", res.Recipe)
		for _, u := range res.URLs {
			fmt.Fprintf(w, "  Open %s\n", u)
		}
		fmt.Fprintf(w, "Stop it with ugt stop %s.\n", res.Recipe)
		return
	}
	fmt.Fprintf(w, "Done: %s ran and its check passed, in %.0fs.\nResults in %s:\n", res.Recipe, res.Seconds, res.Results)
	for _, f := range res.Files {
		fmt.Fprintf(w, "  %s\n", f)
	}
}

// list: ugt list
func (c *CLI) list(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt list", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	held, err := c.App.List(ctx)
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitBroken
	}
	if *asJSON {
		if held == nil {
			held = []app.Installed{}
		}
		return c.printJSON(held)
	}
	if len(held) == 0 {
		fmt.Fprintln(c.Stdout, "UGT holds nothing on this machine yet.")
		return ExitOK
	}
	for _, in := range held {
		state := "verified here, not run"
		switch {
		case in.Running:
			state = "running"
		case in.Kind == "service":
			state = "stopped"
		case len(in.Workspaces) > 0:
			state = "ran here"
		}
		fmt.Fprintf(c.Stdout, "%s (%s): %s, %s, %s\n", in.Recipe, state, count(len(in.Images), "image"),
			count(len(in.Volumes), "volume")+" of saved data", count(len(in.Verifications), "verification workspace"))
		if in.Results != "" {
			fmt.Fprintf(c.Stdout, "  results in %s\n", in.Results)
		}
	}
	return ExitOK
}

func (c *CLI) stop(ctx context.Context, args []string) int {
	return c.change(ctx, "stop", args, false, c.App.Stop)
}

func (c *CLI) remove(ctx context.Context, args []string) int {
	return c.change(ctx, "remove", args, false, c.App.Remove)
}

func (c *CLI) deleteData(ctx context.Context, args []string) int {
	return c.change(ctx, "delete-data", args, true, c.App.DeleteData)
}

// change runs stop, remove or delete-data. Deleting data is for good, so it
// takes --yes: consent given by flag, never a prompt.
func (c *CLI) change(ctx context.Context, name string, args []string, consent bool, do func(context.Context, string) (app.Change, error)) int {
	fs := flag.NewFlagSet("ugt "+name, flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	yes := fs.Bool("yes", false, "agree to deleting the saved data for good")
	if err := parseInterspersed(fs, args); err != nil || fs.NArg() != 1 {
		if err == nil {
			fmt.Fprintln(c.Stderr, "usage: ugt "+name+" "+commands()[name].usage)
		}
		return ExitUsage
	}
	if consent && !*yes {
		fmt.Fprintf(c.Stderr, "ugt: %s deletes %s's saved data for good; run it again with --yes to agree\n", name, fs.Arg(0))
		return ExitUsage
	}
	ch, err := do(ctx, fs.Arg(0))
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitFailed
	}
	if *asJSON {
		return c.printJSON(ch)
	}
	for _, d := range ch.Done {
		fmt.Fprintf(c.Stdout, "%s: %s\n", ch.Recipe, d)
	}
	for _, k := range ch.Kept {
		fmt.Fprintf(c.Stdout, "Kept %s\n", k)
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
	from := fs.String("from", "", "resume the last verification at this step (sample, task, check or sample-check), with its image")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := parseInterspersed(fs, args); err != nil || fs.NArg() != 1 {
		if err == nil {
			fmt.Fprintln(c.Stderr, "usage: ugt recipe verify "+commands()["recipe verify"].usage)
		}
		return ExitUsage
	}
	opts := app.VerifyOptions{Images: images, From: *from}
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

// printFailure explains a failed step: the diagnosis when one matched, the
// last output otherwise.
func (c *CLI) printFailure(step report.Step, seconds float64) {
	w := c.Stdout
	fmt.Fprintf(w, "FAILED at %s (exit %d) after %.0fs\n", step.Name, step.Exit, seconds)
	for _, f := range step.Findings {
		fmt.Fprintf(w, "\n  Why: %s\n  Fix: %s\n", f.Why, f.Fix)
		if f.Line != "" {
			fmt.Fprintf(w, "  Seen: %s\n", f.Line)
		}
	}
	if len(step.Findings) == 0 {
		fmt.Fprintf(w, "\nLast output:\n%s\n", indent(step.Log))
	}
}

func (c *CLI) printReport(rep report.Report) {
	w := c.Stdout
	fmt.Fprintln(w)
	if step, failed := rep.FailedStep(); failed {
		c.printFailure(step, rep.Seconds)
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
	if rep.From != "" {
		fmt.Fprintf(w, "Resumed from %s with the last verification's image; verify in full before sharing.\n", rep.From)
	}
}

// count says "1 image" or "3 images".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}
