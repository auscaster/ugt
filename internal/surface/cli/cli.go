// Package cli is the ugt command line: a thin surface over the app's use
// cases. Every command prints for people by default and takes --json for
// agents and CI.
package cli

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/admission"
	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/job"
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
	Layer   string // the Linux layer this ugt runs recipes on
	// MCP serves the MCP surface over stdio, for ugt mcp; the composition
	// root provides it.
	MCP func(ctx context.Context) error
	// Open serves UGT's own page for a ugt:// link, for ugt open; the
	// composition root provides it.
	Open func(ctx context.Context, link string) error
}

type command struct {
	usage string
	run   func(c *CLI, ctx context.Context, args []string) int
}

// commands is built on demand: its entries read it back for their usage.
func commands() map[string]command {
	return map[string]command{
		"recipe new":      {"[--from FILE|URL|PATH] [--blank] [--kind job|service] [--what TEXT] [--dir DIR] [--json] OWNER/REPO TASK", (*CLI).newRecipe},
		"recipe validate": {"[--admit] [--json] RECIPE_DIR...", (*CLI).validate},
		"recipe verify":   {"[--pull [SUFFIX=]REF]... [--from STEP] [--allow] [--json] RECIPE_DIR", (*CLI).verify},
		"recipe exec":     {"[--json] RECIPE_DIR SERVICE [-- COMMAND [ARG]...]", (*CLI).exec},
		"recipe submit":   {"[--agent NAME] [--dry-run] [--json] RECIPE_DIR", (*CLI).submit},
		"version":         {"[--json]", (*CLI).version},
		"update":          {"[--json]", (*CLI).update},
		"open":            {"LINK   (what an Open in UGT link from a web page runs)", (*CLI).open},
		"status":          {"[--json] [JOB]", (*CLI).status},
		"mcp":             {"", (*CLI).mcp},
		"command":         {"NAME [ARG]...   (what a command's launcher runs)", (*CLI).command},
		"setup":           {"[--yes] [--json]", (*CLI).setup},
		"doctor":          {"[--json]", (*CLI).doctor},
		"connect":         {"[--json] [APP]...", (*CLI).connect},
		"settings set":    {"RECIPE NAME   (reads the value from the terminal, or from a pipe)", (*CLI).settingsSet},
		"settings list":   {"[--json] RECIPE", (*CLI).settingsList},
		"settings forget": {"RECIPE NAME", (*CLI).settingsForget},
		"disconnect":      {"[--json] [APP]...", (*CLI).disconnect},
		"uninstall":       {"[--yes] [--json]", (*CLI).uninstall},
		"find":            {"[--json] OWNER/REPO[/TASK]|WORDS...", (*CLI).find},
		"run":             {"[--input FILE|NAME=FILE]... [--to DIR] [--set NAME=VALUE]... [--instance NAME] [--allow] [--json] OWNER/REPO [TASK]", (*CLI).run},
		"list":            {"[--json]", (*CLI).list},
		"stop":            {"[--json] RECIPE", (*CLI).stop},
		"remove":          {"[--json] RECIPE", (*CLI).remove},
		"delete-data":     {"--yes [--json] RECIPE", (*CLI).deleteData},
		"allow":           {"[--yes] [--json] RECIPE", (*CLI).allow},
		"login":           {"[--json]", (*CLI).login},
		"logout":          {"", (*CLI).logout},
		"reports":         {"[on|off]", (*CLI).reports},
	}
}

// Run runs one command line and returns its exit status. A command run in a
// terminal asks the user there; ugt mcp keeps the desktop's prompt, because
// its stdin belongs to the AI app.
func (c *CLI) Run(ctx context.Context, args []string) int {
	for n := 2; n >= 1; n-- {
		if len(args) >= n {
			name := strings.Join(args[:n], " ")
			if cmd, ok := commands()[name]; ok {
				if t, ok := c.terminal(); ok && name != "mcp" {
					c.App.Prompt = t
				}
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
	if t, ok := c.terminal(); ok {
		q := app.Question{Title: "UGT", Message: fmt.Sprintf("%s (%s)", s.Name, s.What)}
		if s.How != "" {
			q.Message += "\n  " + s.How
		}
		if s.Kind == "secret" {
			return t.Secret(context.Background(), q)
		}
		return t.line(q.Message)
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

// mcp: ugt mcp serves agents over stdio until the client disconnects, with
// the tools of the MCP servers recipes provide among UGT's own.
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

// command: ugt command NAME [ARG]... runs a command a recipe provides, in
// this folder, as its launcher on the PATH does. The arguments are the
// command's own, so ugt reads none of them; it exits with the command's
// status.
func (c *CLI) command(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(c.Stderr, "usage: ugt command "+commands()["command"].usage)
		return ExitUsage
	}
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitBroken
	}
	_, terminal := c.terminal()
	if f, ok := c.Stdout.(*os.File); !ok || !term.IsTerminal(int(f.Fd())) {
		terminal = false
	}
	to := app.Attached{Folder: dir, Terminal: terminal, Stdin: c.Stdin, Stdout: c.Stdout, Stderr: c.Stderr}
	exit, err := c.App.RunCommand(ctx, args[0], args[1:], to)
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitBroken
	}
	return exit
}

func (c *CLI) version(_ context.Context, args []string) int {
	if len(args) == 1 && args[0] == "--json" {
		return c.printJSON(map[string]string{"version": c.Version, "layer": c.Layer})
	}
	fmt.Fprintf(c.Stdout, "ugt %s\nlayer %s\n", c.Version, c.Layer)
	return ExitOK
}

// open: ugt open LINK is what the system runs for a ugt:// link from a web
// page. It opens UGT's own page for the link's recipe, which runs nothing
// until the user starts it there, and stops once the page has gone quiet.
func (c *CLI) open(ctx context.Context, args []string) int {
	if len(args) != 1 || c.Open == nil {
		fmt.Fprintln(c.Stderr, "usage: ugt open "+commands()["open"].usage)
		return ExitUsage
	}
	if err := c.Open(ctx, args[0]); err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitBroken
	}
	return ExitOK
}

// update: ugt update replaces this ugt with the newest release, then runs
// the new ugt's setup, so the engine runs the layer the new ugt was built
// with. A package manager's ugt is that manager's to update.
func (c *CLI) update(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt update", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	res, err := c.App.Update(ctx)
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitBroken
	}
	if !res.Updated {
		if *asJSON {
			return c.printJSON(res)
		}
		if res.Next != "" {
			fmt.Fprintf(c.Stdout, "A package manager installed this ugt: update it with %s\n", res.Next)
		} else {
			fmt.Fprintf(c.Stdout, "UGT %s is the newest release.\n", res.From)
		}
		return ExitOK
	}
	setup := exec.CommandContext(ctx, res.Path, "setup")
	setup.Stderr = c.Stderr
	var setupJSON json.RawMessage
	if *asJSON {
		setup.Args = append(setup.Args, "--json")
		setupJSON, err = setup.Output()
	} else {
		fmt.Fprintf(c.Stdout, "Updated UGT from %s to %s. AI apps start the new one the next time they start UGT.\n", res.From, res.Latest)
		setup.Stdout = c.Stdout
		err = setup.Run()
	}
	if *asJSON {
		c.printJSON(struct {
			app.UpdateResult
			Setup json.RawMessage `json:"setup,omitempty"`
		}{res, setupJSON})
	}
	if err != nil {
		if !*asJSON {
			fmt.Fprintf(c.Stderr, "ugt: the new ugt's setup: %v\n", err)
		}
		return ExitFailed
	}
	return ExitOK
}

// status: ugt status [JOB] reports one job, or the recent ones: the long
// operations agents start through MCP, which the CLI runs in the foreground.
func (c *CLI) status(_ context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt status", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := parseInterspersed(fs, args); err != nil || fs.NArg() > 1 {
		return ExitUsage
	}
	var jobs []job.Job
	if fs.NArg() == 1 {
		j, err := c.App.Job(fs.Arg(0))
		if err != nil {
			fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
			return ExitFailed
		}
		jobs = []job.Job{j}
	} else {
		var err error
		if jobs, err = c.App.RecentJobs(20); err != nil {
			fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
			return ExitBroken
		}
	}
	if *asJSON {
		if fs.NArg() == 1 {
			return c.printJSON(jobs[0])
		}
		if jobs == nil {
			jobs = []job.Job{}
		}
		return c.printJSON(jobs)
	}
	if len(jobs) == 0 {
		fmt.Fprintln(c.Stdout, "No jobs yet: agents start them through ugt mcp.")
	}
	for _, j := range jobs {
		fmt.Fprintf(c.Stdout, "%s  %-6s %-9s %s  %s\n", j.ID, j.Kind, j.State, j.Started.Format("2 Jan 15:04"), j.Recipe)
		if j.Error != "" {
			fmt.Fprintf(c.Stdout, "  %s\n", j.Error)
		}
		if fs.NArg() == 1 {
			for _, e := range j.Events {
				fmt.Fprintf(c.Stdout, "  %s %-13s %s\n", e.Time.Format("15:04:05"), e.Step, e.Message)
			}
		}
	}
	return ExitOK
}

// validate: ugt recipe validate [--json] RECIPE_DIR...; a recipe.json path is
// read as its folder.
func (c *CLI) validate(_ context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt recipe validate", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	asJSON := fs.Bool("json", false, "print the results as JSON; with --admit, the admission records (spec/admission/v0)")
	admit := fs.Bool("admit", false, "also check each folder for the catalogue, as recipes/<owner>/<repo>/<task>: filed under its ID")
	if err := parseInterspersed(fs, args); err != nil || fs.NArg() == 0 {
		if err == nil {
			c.usage()
		}
		return ExitUsage
	}
	type result struct {
		Recipe string `json:"recipe"`
		Valid  bool   `json:"valid"`
		Error  string `json:"error,omitempty"`
	}
	var results []result
	var records []admission.Record
	status := ExitOK
	for _, target := range fs.Args() {
		dir := target
		if filepath.Base(target) == "recipe.json" {
			dir = filepath.Dir(target)
		}
		r := result{Recipe: target, Valid: true}
		if *admit {
			rec, err := c.App.Admit(dir)
			if err == nil && len(rec.Problems) > 0 {
				err = errors.New(strings.Join(rec.Problems, "\n"))
			}
			if err != nil {
				r.Valid, r.Error = false, err.Error()
			}
			records = append(records, rec)
		} else if err := c.App.Validate(dir); err != nil {
			r.Valid, r.Error = false, err.Error()
		}
		if !r.Valid {
			status = ExitFailed
		}
		results = append(results, r)
		if *asJSON {
			continue
		}
		if r.Valid {
			fmt.Fprintf(c.Stdout, "ok       %s\n", target)
		} else {
			fmt.Fprintf(c.Stdout, "invalid  %s\n%s\n", target, r.Error)
		}
	}
	switch {
	case *asJSON && *admit:
		c.printJSON(records)
	case *asJSON:
		c.printJSON(results)
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
	asJSON := len(before) > 0 && before[0] == "--json"
	if asJSON {
		before = before[1:]
	}
	if len(before) != 2 {
		fmt.Fprintln(c.Stderr, "usage: ugt recipe exec "+commands()["recipe exec"].usage)
		return ExitUsage
	}
	var output strings.Builder
	var out io.Writer = c.Stdout
	if asJSON {
		out = &output
	}
	res, err := c.App.Exec(ctx, before[0], before[1], command, out)
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitBroken
	}
	if asJSON {
		c.printJSON(map[string]any{"exit": res.Exit, "service": res.Service, "workspace": res.Workspace, "image": res.Image, "output": output.String()})
		return res.Exit
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
		state := ""
		switch {
		case e.Problem != "":
			state = ", does not validate yet"
		case e.Here:
			state = ", verified on this machine"
		case e.Source == catalogue.Local:
			state = ", not verified yet"
		}
		fmt.Fprintf(c.Stdout, "%-40s %s\n  %s (%s%s)\n", e.Recipe, e.Kind, e.What, e.Source, state)
		if strings.EqualFold(strings.TrimSpace(res.Query), e.Recipe) {
			for _, s := range e.Settings {
				fmt.Fprintf(c.Stdout, "  %s: %s %s\n", s.Name, s.What, s.How)
			}
			for _, issue := range e.KnownIssues {
				fmt.Fprintf(c.Stdout, "  limit: %s\n", issue)
			}
		}
		if p := provided(e.Provides); p != "" {
			fmt.Fprintf(c.Stdout, "  provides: %s\n", p)
		}
		if kinds := accessKinds(e.Access); kinds != "" {
			fmt.Fprintf(c.Stdout, "  needs: %s\n", kinds)
		}
	}
	for _, n := range res.Next {
		fmt.Fprintln(c.Stdout, n)
	}
	return ExitOK
}

// provided names what a recipe provides, as "2 pages, the command jq".
func provided(p recipe.Provides) string {
	var out []string
	if n := len(p.Pages); n > 0 {
		out = append(out, count(n, "page"))
	}
	if n := len(p.Endpoints); n > 0 {
		out = append(out, count(n, "endpoint"))
	}
	for _, c := range p.Commands {
		out = append(out, "the command "+c.Name)
	}
	if n := len(p.MCPServers); n > 0 {
		out = append(out, count(n, "MCP server"))
	}
	return strings.Join(out, ", ")
}

// accessKinds names the access a recipe declares, as "internet, device".
func accessKinds(declared []recipe.Access) string {
	kinds := make([]string, len(declared))
	for i, a := range declared {
		kinds[i] = a.Kind
	}
	return strings.Join(kinds, ", ")
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
	instance := fs.String("instance", "", "name of an independent copy of this service")
	values := settingValues{}
	fs.Var(&inputs, "input", "an input file, as FILE or NAME=FILE; a bare name is looked for in the UGT folder")
	fs.Var(values, "set", "a value setting, as NAME=VALUE; secrets are never given here")
	to := fs.String("to", "", "where the results go; the recipe's folder in the UGT folder when not given")
	allow := fs.Bool("allow", false, "agree to the access beyond the sandbox the recipe declares, without being asked")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := parseInterspersed(fs, args); err != nil || fs.NArg() < 1 || fs.NArg() > 2 {
		if err == nil {
			fmt.Fprintln(c.Stderr, "usage: ugt run "+commands()["run"].usage)
		}
		return ExitUsage
	}
	opts := app.RunOptions{Project: fs.Arg(0), Task: fs.Arg(1), Instance: *instance, Inputs: inputs, To: *to, Values: values, Allow: *allow}
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
	switch {
	case res.Kind == recipe.KindService:
		fmt.Fprintf(w, "Running %s; its check passed.\n", res.Recipe)
	case res.Results != "":
		fmt.Fprintf(w, "Done: %s ran and its check passed, in %.0fs.\nResults in %s:\n", res.Recipe, res.Seconds, res.Results)
		for _, f := range res.Files {
			fmt.Fprintf(w, "  %s\n", f)
		}
	default:
		fmt.Fprintf(w, "Ready: %s passed its check on its sample, in %.0fs.\n", res.Recipe, res.Seconds)
	}
	for _, p := range res.Pages {
		fmt.Fprintf(w, "Open %s\n  %s\n", p.URL, p.What)
	}
	for _, e := range res.Endpoints {
		fmt.Fprintf(w, "Connect to %s\n  %s\n", e.URL, e.What)
	}
	for _, cmd := range res.Commands {
		fmt.Fprintf(w, "Type %s\n  %s\n", cmd.Type, cmd.What)
		if cmd.Note != "" {
			fmt.Fprintf(w, "  %s\n", cmd.Note)
		}
	}
	for _, m := range res.MCPServers {
		fmt.Fprintf(w, "Ask your AI app\n  %s\n  %s\n", m.What, m.Note)
	}
	if res.Kind == recipe.KindService {
		target := cmp.Or(res.Installation, res.Recipe)
		fmt.Fprintf(w, "Stop it with ugt stop %s.\n", target)
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
		case in.Kind == recipe.KindService:
			state = "stopped"
		case len(in.Workspaces) > 0:
			state = "ran here"
		}
		fmt.Fprintf(c.Stdout, "%s (%s): %s, %s, %s\n", cmp.Or(in.Installation, in.Recipe), state, count(len(in.Images), "image"),
			count(len(in.Volumes), "volume")+" of saved data", count(len(in.Verifications), "verification workspace"))
		if in.Results != "" {
			fmt.Fprintf(c.Stdout, "  results in %s\n", in.Results)
		}
		if len(in.Commands) > 0 {
			fmt.Fprintf(c.Stdout, "  commands: %s\n", strings.Join(in.Commands, ", "))
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
		fmt.Fprintf(c.Stdout, "%s: %s\n", cmp.Or(ch.Installation, ch.Recipe), d)
	}
	for _, k := range ch.Kept {
		fmt.Fprintf(c.Stdout, "Kept %s\n", k)
	}
	return ExitOK
}

// submit: ugt recipe submit [--agent NAME] [--dry-run] RECIPE_DIR
func (c *CLI) submit(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt recipe submit", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	var opts app.SubmitOptions
	fs.StringVar(&opts.Agent, "agent", "", "who made and verified the recipe, for the pull request: an agent and model, or your name")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "show what would be proposed, and change nothing")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := parseInterspersed(fs, args); err != nil || fs.NArg() != 1 {
		if err == nil {
			fmt.Fprintln(c.Stderr, "usage: ugt recipe submit "+commands()["recipe submit"].usage)
		}
		return ExitUsage
	}
	if !*asJSON {
		opts.Progress = func(step, message string) { fmt.Fprintf(c.Stdout, "%-13s %s\n", step, message) }
	}
	res, err := c.App.Submit(ctx, fs.Arg(0), opts)
	if *asJSON {
		c.printJSON(res)
	} else {
		w := c.Stdout
		if step, failed := res.Report.FailedStep(); failed {
			c.printFailure(step, res.Report.Seconds)
		}
		if len(res.Leaks) > 0 {
			fmt.Fprintln(w, "\nThese must not be shared; fix each, then submit again:")
			for _, l := range res.Leaks {
				fmt.Fprintf(w, "  - %s\n", l)
			}
		}
		if res.Title != "" && (res.DryRun || res.PullRequest != "") {
			fmt.Fprintf(w, "\n%s\nFiles: %s\n", res.Title, strings.Join(res.Files, ", "))
		}
		if res.DryRun && err == nil {
			fmt.Fprintf(w, "\n%s\nA dry run: nothing was changed or sent. Run again without --dry-run to open the pull request.\n", res.Body)
		}
		if res.PullRequest != "" {
			fmt.Fprintf(w, "Opened %s\n", res.PullRequest)
		}
	}
	switch {
	case errors.Is(err, app.ErrNotShareable):
		return ExitFailed
	case err != nil:
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
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
	from := fs.String("from", "", "resume the last verification at this step (sample, task, check or sample-check), with its image")
	allow := fs.Bool("allow", false, "agree to the access beyond the sandbox the recipe declares, without being asked")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := parseInterspersed(fs, args); err != nil || fs.NArg() != 1 {
		if err == nil {
			fmt.Fprintln(c.Stderr, "usage: ugt recipe verify "+commands()["recipe verify"].usage)
		}
		return ExitUsage
	}
	opts := app.VerifyOptions{Images: images, From: *from, Allow: *allow}
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

// allow: ugt allow [--yes] [--json] RECIPE. Agrees, ahead of a run, to the
// access beyond the sandbox a recipe's runs need, for an AI app on a machine
// where UGT cannot ask during the run.
func (c *CLI) allow(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt allow", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	yes := fs.Bool("yes", false, "agree without being asked")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := parseInterspersed(fs, args); err != nil || fs.NArg() != 1 {
		if err == nil {
			fmt.Fprintln(c.Stderr, "usage: ugt allow "+commands()["allow"].usage)
		}
		return ExitUsage
	}
	res, err := c.App.Allow(ctx, fs.Arg(0), *yes)
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitFailed
	}
	if *asJSON {
		return c.printJSON(res)
	}
	if len(res.Grants) == 0 {
		fmt.Fprintf(c.Stdout, "%s needs no access beyond the sandbox to run.\n", res.Recipe)
		return ExitOK
	}
	fmt.Fprintf(c.Stdout, "Allowed %s:\n", res.Recipe)
	for _, g := range res.Grants {
		fmt.Fprintf(c.Stdout, "  %s\n", g)
	}
	return ExitOK
}

// terminal is the prompt of the terminal the command line runs in, when it
// has one.
func (c *CLI) terminal() (terminalPrompt, bool) {
	f, ok := c.Stdin.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return terminalPrompt{}, false
	}
	return terminalPrompt{in: f, out: c.Stderr}, true
}

// terminalPrompt asks the user in their terminal.
type terminalPrompt struct {
	in  *os.File
	out io.Writer
}

var _ app.Prompt = terminalPrompt{}

func (t terminalPrompt) Confirm(_ context.Context, q app.Question) (bool, error) {
	answer, err := t.line(q.Message + " [y/N]")
	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes", err
}

func (t terminalPrompt) Secret(_ context.Context, q app.Question) (string, error) {
	fmt.Fprintf(t.out, "%s\n> ", q.Message)
	b, err := term.ReadPassword(int(t.in.Fd()))
	fmt.Fprintln(t.out)
	return strings.TrimSpace(string(b)), err
}

func (t terminalPrompt) line(prompt string) (string, error) {
	fmt.Fprintf(t.out, "%s\n> ", prompt)
	line, err := bufio.NewReader(t.in).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// login: ugt login [--json]. Signs in to GitHub with its device flow, which
// sharing a recipe needs: the user approves a code in their browser, and the
// token stays in the system's secret store.
func (c *CLI) login(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ugt login", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	res, err := c.App.Login(ctx, func(l app.DeviceLogin) {
		fmt.Fprintf(c.Stderr, "To sign in to GitHub, open %s and enter %s\n", l.URL, l.Code)
	})
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitFailed
	}
	if *asJSON {
		return c.printJSON(res)
	}
	fmt.Fprintf(c.Stdout, "Signed in to GitHub as %s.\n", res.User)
	return ExitOK
}

// reports: ugt reports [on|off]. Shows or changes whether this machine tells
// the catalogue how its recipes' runs go.
func (c *CLI) reports(ctx context.Context, args []string) int {
	if len(args) > 1 || (len(args) == 1 && args[0] != "on" && args[0] != "off") {
		return ExitUsage
	}
	if len(args) == 1 {
		if err := c.App.SetReporting(args[0] == "on"); err != nil {
			fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
			return ExitFailed
		}
	}
	state, err := c.App.Reporting()
	if err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitFailed
	}
	switch state {
	case "on":
		fmt.Fprintln(c.Stdout, "Run reports are on: after each run of a catalogue recipe, UGT tells the catalogue whether it worked, and where it failed. Nothing of yours is sent.")
	case "off":
		fmt.Fprintln(c.Stdout, "Run reports are off: UGT sends nothing about your runs.")
	default:
		fmt.Fprintln(c.Stdout, "Run reports are off until you choose: UGT asks the first time a catalogue recipe fails here.")
	}
	return ExitOK
}

// logout: ugt logout. Forgets the GitHub login.
func (c *CLI) logout(ctx context.Context, args []string) int {
	if len(args) != 0 {
		return ExitUsage
	}
	if err := c.App.Logout(ctx); err != nil {
		fmt.Fprintf(c.Stderr, "ugt: %v\n", err)
		return ExitFailed
	}
	fmt.Fprintln(c.Stdout, "Signed out of GitHub.")
	return ExitOK
}
