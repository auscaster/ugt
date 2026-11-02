package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/auscaster/ugt/internal/domain/access"
	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/compose"
	"github.com/auscaster/ugt/internal/domain/ledger"
	"github.com/auscaster/ugt/internal/domain/placement"
	"github.com/auscaster/ugt/internal/domain/recipe"
	"github.com/auscaster/ugt/internal/domain/report"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// RunOptions says which recipe to run, and on what.
type RunOptions struct {
	Project string // owner/repo, or a github.com link into it
	Task    string // the task, when the project has several recipes
	// Inputs are the user's files, as FILE or NAME=FILE, where NAME is an
	// input's path in recipe.json or its file name. A file not found as given
	// is looked for in the Home folder, so a bare name works there.
	Inputs []string
	// Texts are inputs given as text rather than files, by input name: a
	// script, a list of URLs. They let an agent with no file access of its
	// own, as in a desktop chat app, run a recipe whose input it writes.
	Texts  map[string]string
	To     string            // where results go; Home/<owner>/<repo>/<task> when empty
	Values map[string]string // value settings, by name
	// Allow is the user's agreement, given on the command line, to the access
	// beyond the sandbox the recipe declares that UGT asks about (see
	// consent). Without it UGT asks the user itself, the first time and
	// whenever that access changes.
	Allow    bool
	Progress func(step, message string)
}

// RunResult is a finished run, or a service left running, with everything
// it gives the user ready to use.
type RunResult struct {
	Recipe   string           `json:"recipe"`
	Kind     string           `json:"kind"`
	Source   catalogue.Source `json:"source"`
	Revision string           `json:"revision"`
	// Passed is a run that proved itself here and set up what it provides: a
	// job's task and check passed, or a service is up and its check passed.
	Passed     bool           `json:"passed"`
	Steps      []report.Step  `json:"steps"`
	Results    string         `json:"results,omitempty"`     // where the outputs were delivered
	Files      []string       `json:"files,omitempty"`       // the delivered files, relative to Results
	Pages      []Reachable    `json:"pages,omitempty"`       // to open in the browser
	Endpoints  []Reachable    `json:"endpoints,omitempty"`   // for other software
	Commands   []CommandReady `json:"commands,omitempty"`    // to type in the terminal
	MCPServers []MCPReady     `json:"mcp_servers,omitempty"` // added to the user's AI apps
	Seconds    float64        `json:"seconds"`
}

// NotPlaced is a run refused because the machine cannot hold the recipe.
type NotPlaced struct{ placement.Decision }

func (e *NotPlaced) Error() string { return e.Reason + " " + e.Fix }

// Run gets a recipe working for the user: it finds the recipe, checks the
// machine can hold it, pulls or builds its images and copies the inputs in.
// A job then runs its task and check on them and delivers the outputs to the
// Home folder, or, when it delivers none, proves itself on its sample; its
// stack is then taken down. A service starts, passes its check and keeps
// running, its pages and endpoints at addresses that stay the same. Last,
// the commands and MCP servers the recipe provides are set up. A project
// with no recipe returns *catalogue.NoRecipe.
func (a *App) Run(ctx context.Context, opts RunOptions) (res RunResult, err error) {
	progress := opts.Progress
	if progress == nil {
		progress = func(string, string) {}
	}
	started := a.Now()
	defer func() { res.Seconds = a.Now().Sub(started).Seconds() }()

	owner, name, err := recipe.ParseProject(opts.Project)
	if err != nil {
		return RunResult{}, err
	}
	entry, src, err := a.resolve(ctx, owner, name, opts.Task)
	if err != nil {
		return RunResult{}, err
	}
	f, err := recipe.Load(src)
	if err != nil {
		return RunResult{}, fmt.Errorf("%s (%s): %w", entry.Recipe, entry.Source, err)
	}
	rev, err := recipe.Revision(src)
	if err != nil {
		return RunResult{}, err
	}
	run, err := recipe.RunRevision(src)
	if err != nil {
		return RunResult{}, err
	}
	res = RunResult{Recipe: f.ID.String(), Kind: f.Recipe.Kind, Source: entry.Source, Revision: rev}
	texts, err := textInputs(f.Recipe, opts.Texts)
	if err != nil {
		return res, err
	}
	inputs, err := a.inputFiles(f.Recipe, opts.Inputs, texts)
	if err != nil {
		return res, err
	}
	env, secrets, err := a.settingsEnv(ctx, f.Recipe, opts.Values)
	if err != nil {
		return res, err
	}
	status, err := a.Runtime.Status(ctx)
	if err != nil {
		return res, err
	}
	if !status.Ready {
		return res, fmt.Errorf("the engine is not ready: %s %s", status.Problem, status.Fix)
	}
	info, err := a.Engine.Info(ctx)
	if err != nil {
		return res, err
	}
	if entry.Source == catalogue.Published {
		defer func() {
			if err == nil {
				a.reportRun(ctx, res, info.Arch)
			}
		}()
	}

	files := a.Runtime.Files()
	ws := a.Layout.Workspace(f.ID)
	// an earlier run may still be up, with the workspace's folders mounted:
	// take it down, keeping its volumes and its addresses, before replacing them
	var ports map[string]int
	if last, earlier, err := readSession(files, ws); err == nil {
		if err := a.Engine.Down(ctx, earlier, false, io.Discard); err != nil {
			return res, fmt.Errorf("stop the earlier run of %s: %w", f.ID, err)
		}
		ports = last.Ports
	}
	for _, d := range []string{workspace.InputsDir, workspace.OutputsDir} {
		if err := files.RemoveAll(path.Join(ws, d)); err != nil {
			return res, err
		}
	}
	if err := prepareDirs(files, ws); err != nil {
		return res, err
	}
	if err := stage(files, src, path.Join(ws, workspace.RecipeDir)); err != nil {
		return res, err
	}
	env[recipe.VarImage], env[recipe.VarArch] = f.ID.Image(run), info.Arch
	project := Project{Name: f.ID.Project(), Dir: ws, Files: []string{path.Join(workspace.RecipeDir, recipe.ComposeFile)}, Env: env}
	cfg, err := a.Engine.Config(ctx, project)
	if err != nil {
		return res, fmt.Errorf("read compose.yaml: %w", err)
	}
	if err := checkAccess(f, cfg, ws); err != nil {
		return res, err
	}
	if err := a.consent(ctx, f, entry.Source, runGrants(f, cfg, ws), opts.Allow); err != nil {
		return res, err
	}
	if d := placement.Decide(needs(cfg), placement.Host{Runtime: a.Runtime.Name(), MemoryBytes: info.MemoryBytes}); !d.Run {
		return res, &NotPlaced{d}
	}
	for _, in := range inputs {
		if err := files.MkdirAll(path.Dir(path.Join(ws, in.path))); err != nil {
			return res, err
		}
		if err := files.CopyIn(in.host, path.Join(ws, in.path)); err != nil {
			return res, fmt.Errorf("copy %s in: %w", in.host, err)
		}
	}
	for p, text := range texts {
		if err := files.MkdirAll(path.Dir(path.Join(ws, p))); err != nil {
			return res, err
		}
		if err := files.WriteFile(path.Join(ws, p), []byte(text)); err != nil {
			return res, err
		}
	}
	publish, ports, err := a.publishing(f.Recipe.Provides, ports)
	if err != nil {
		return res, err
	}
	if err := a.writeOverride(files, ws, cfg, f.ID.String(), run, info.CPUs, publish); err != nil {
		return res, err
	}
	project.Files = append(project.Files, compose.OverrideFile)
	if err := writeSession(files, project, f.Recipe, rev, secrets, ports); err != nil {
		return res, err
	}
	_ = a.record(ledger.Entry{Kind: ledger.Workspace, Name: ws, Recipe: f.ID.String()})
	_ = a.record(ledger.Entry{Kind: ledger.Project, Name: project.Name, Recipe: f.ID.String()})

	st := &steps{files: files, dir: ws, now: a.Now, progress: progress}
	defer func() { res.Steps = st.done }()
	if missing, err := a.missingImages(ctx, cfg); err != nil {
		return res, err
	} else if len(missing) > 0 {
		if pulls := published(entry, missing, f.ID.Image(run), info.Arch); pulls != nil {
			if !st.run(report.StepPull, func(w io.Writer) (int, error) { return a.pull(ctx, pulls, w) }) {
				return res, nil
			}
		} else {
			progress(report.StepBuild, buildEstimate(entry))
			if !st.run(report.StepBuild, func(w io.Writer) (int, error) { return a.Engine.Build(ctx, project, w) }) {
				return res, nil
			}
		}
		for _, image := range missing {
			_ = a.record(ledger.Entry{Kind: ledger.Image, Name: image, Recipe: f.ID.String()})
		}
	}

	service := func(name string) func(io.Writer) (int, error) {
		return func(w io.Writer) (int, error) { return a.Engine.Run(ctx, project, name, nil, w) }
	}
	switch {
	case f.Recipe.Kind == recipe.KindService:
		if !st.run(report.StepTask, func(w io.Writer) (int, error) { return a.up(ctx, project, w) }) ||
			!st.run(report.StepCheck, service(recipe.ServiceCheck)) {
			return res, nil
		}
		res.Pages = reachable(f.Recipe.Provides.Pages, ports)
		res.Endpoints = reachable(f.Recipe.Provides.Endpoints, ports)
	case len(f.Recipe.Outputs) > 0:
		defer a.takeDown(ctx, project)
		if !st.run(report.StepTask, service(recipe.ServiceTask)) || !st.run(report.StepCheck, service(recipe.ServiceCheck)) {
			return res, nil
		}
		if err := a.deliver(files, ws, opts.To, f.ID, &res); err != nil {
			return res, err
		}
	default:
		// a job that only provides commands or MCP servers proves itself on
		// its sample before they are set up; each step runs its service
		defer a.takeDown(ctx, project)
		for _, step := range runSteps {
			if !st.run(step, service(step)) {
				return res, nil
			}
		}
	}
	if err := a.provide(ctx, f, &res); err != nil {
		return res, err
	}
	res.Passed = true
	return res, nil
}

// takeDown removes a job's stack once it is done, keeping its volumes.
func (a *App) takeDown(ctx context.Context, p Project) {
	var discard strings.Builder
	_ = a.Engine.Down(context.WithoutCancel(ctx), p, false, &discard)
}

// deliver copies a job's outputs to where the user asked, or to the
// recipe's folder in Home.
func (a *App) deliver(files Files, ws, to string, id recipe.ID, res *RunResult) error {
	res.Results = to
	if res.Results == "" {
		res.Results = a.Layout.Results(id)
	}
	outputs := path.Join(ws, workspace.OutputsDir)
	produced, err := files.List(outputs)
	if err != nil {
		return err
	}
	if err := files.CopyOut(outputs, res.Results); err != nil {
		return fmt.Errorf("deliver the results to %s: %w", res.Results, err)
	}
	for _, p := range produced {
		res.Files = append(res.Files, strings.TrimPrefix(p.Path, outputs+"/"))
	}
	return nil
}

// runGrants are the grants a run makes, which the user agrees to: those of
// the services it starts, and of the commands and MCP servers it provides.
func runGrants(f *recipe.Folder, cfg compose.Model, ws string) []access.Grant {
	return append(access.In(access.Grants(cfg, ws), runServices(cfg, f.Recipe.Provides)), access.Commands(f.Recipe.Provides)...)
}

// buildEstimate says what building a recipe here costs before it starts, from
// how long its verification took on GitHub's clean machines.
func buildEstimate(e catalogue.Entry) string {
	var longest float64
	for _, v := range e.Verified {
		longest = max(longest, v.Seconds)
	}
	if longest == 0 {
		return "building it on this machine; later runs reuse what it builds"
	}
	return fmt.Sprintf("building it on this machine: about %d min on a fast machine, once; later runs reuse it",
		int(math.Ceil(longest/60)))
}

// resolve finds a recipe across the catalogues and opens it.
func (a *App) resolve(ctx context.Context, owner, name, task string) (catalogue.Entry, fs.FS, error) {
	ix, err := a.project(ctx, owner, name)
	if err != nil {
		return catalogue.Entry{}, nil, err
	}
	entry, err := ix.Resolve(owner, name, task)
	if err != nil {
		return catalogue.Entry{}, nil, err
	}
	if entry.Problem != "" {
		return entry, nil, fmt.Errorf("%s (%s) does not validate yet: %s", entry.Recipe, entry.Source, entry.Problem)
	}
	for _, c := range a.Catalogues {
		if c.Source() == entry.Source {
			src, err := c.Open(ctx, entry.Recipe)
			return entry, src, err
		}
	}
	return entry, nil, fmt.Errorf("no catalogue holds %s", entry.Recipe)
}

// project gathers one project's recipes from every catalogue, in order of
// precedence. A catalogue that cannot be reached does not hide the others; it
// is an error only when nothing was found, so "no recipe" is never claimed
// for a catalogue that was not asked.
func (a *App) project(ctx context.Context, owner, repo string) (catalogue.Index, error) {
	return a.gather(func(c Catalogue) ([]catalogue.Entry, error) { return c.Project(ctx, owner, repo) })
}

// search gathers the recipes whose ID holds every word.
func (a *App) search(ctx context.Context, words []string) (catalogue.Index, error) {
	return a.gather(func(c Catalogue) ([]catalogue.Entry, error) { return c.Search(ctx, words) })
}

func (a *App) gather(ask func(Catalogue) ([]catalogue.Entry, error)) (catalogue.Index, error) {
	var sources [][]catalogue.Entry
	var failed []error
	for _, c := range a.Catalogues {
		entries, err := ask(c)
		if err != nil {
			failed = append(failed, fmt.Errorf("the %s: %w", c.Source(), err))
			continue
		}
		for i := range entries {
			entries[i].Here = entries[i].Problem == "" && a.passedHere(entries[i].Recipe, entries[i].Run)
		}
		sources = append(sources, entries)
	}
	ix := catalogue.Merge(sources...)
	if len(ix) == 0 && len(failed) > 0 {
		return nil, errors.Join(failed...)
	}
	return ix, nil
}

// input is one user file and where it goes in the workspace.
type input struct{ path, host string }

// textLimit bounds one text input: a script or a list, not data.
const textLimit = 1 << 20

// inputNamed finds the declared input a name means: its path, its file name,
// or its file name without the extension when only one input has it.
func inputNamed(r recipe.Recipe, name string) (string, error) {
	var stems []string
	for _, in := range r.Inputs {
		base := path.Base(in.Path)
		if in.Path == name || base == name {
			return in.Path, nil
		}
		if strings.TrimSuffix(base, path.Ext(base)) == name {
			stems = append(stems, in.Path)
		}
	}
	if len(stems) == 1 {
		return stems[0], nil
	}
	if len(r.Inputs) == 0 {
		return "", fmt.Errorf("%s is not an input of %s, which takes no input files", name, r.ID)
	}
	var names []string
	for _, in := range r.Inputs {
		names = append(names, fmt.Sprintf("%s (%s)", path.Base(in.Path), in.What))
	}
	return "", fmt.Errorf("%s is not an input of %s; it takes %s", name, r.ID, strings.Join(names, "; "))
}

// textInputs maps inputs given as text to the paths recipe.json declares.
func textInputs(r recipe.Recipe, texts map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for name, text := range texts {
		target, err := inputNamed(r, name)
		if err != nil {
			return nil, err
		}
		if len(text) > textLimit {
			return nil, fmt.Errorf("%s is over %d MB: give it as a file", name, textLimit>>20)
		}
		out[target] = text
	}
	return out, nil
}

// inputFiles maps the user's files to the inputs recipe.json declares; the
// ones given as text are already placed.
func (a *App) inputFiles(r recipe.Recipe, given []string, texts map[string]string) ([]input, error) {
	declared := make([]string, len(r.Inputs))
	for i, in := range r.Inputs {
		declared[i] = in.Path
	}
	describe := func() string {
		if len(declared) == 0 {
			return "it takes no input files"
		}
		return "it takes " + strings.Join(declared, ", ")
	}
	var out []input
	assigned := map[string]bool{}
	var positional []string
	for _, g := range given {
		name, file, named := strings.Cut(g, "=")
		if !named {
			positional = append(positional, g)
			continue
		}
		target, err := inputNamed(r, name)
		if err != nil {
			return nil, err
		}
		host, err := a.findInput(file)
		if err != nil {
			return nil, err
		}
		out, assigned[target] = append(out, input{target, host}), true
	}
	for _, d := range declared {
		if assigned[d] || texts[d] != "" {
			continue
		}
		if len(positional) == 0 {
			return nil, fmt.Errorf("%s needs %s: %s", r.ID, d, inputWhat(r, d))
		}
		host, err := a.findInput(positional[0])
		if err != nil {
			return nil, err
		}
		out, positional = append(out, input{d, host}), positional[1:]
	}
	if len(positional) > 0 {
		return nil, fmt.Errorf("%s was given more files than it takes; %s", r.ID, describe())
	}
	return out, nil
}

func inputWhat(r recipe.Recipe, p string) string {
	for _, in := range r.Inputs {
		if in.Path == p {
			return in.What
		}
	}
	return ""
}

// findInput finds a user's file as given, or else in the Home folder.
func (a *App) findInput(file string) (string, error) {
	for _, candidate := range []string{file, filepath.Join(a.Layout.Home, file)} {
		if ok, err := a.Disk.Exists(candidate); err != nil {
			return "", err
		} else if ok {
			return filepath.Abs(candidate)
		}
	}
	return "", fmt.Errorf("found no file %s, here or in %s", file, a.Layout.Home)
}

// needs is what a recipe's default services reserve.
func needs(cfg compose.Model) placement.Needs {
	var n placement.Needs
	for _, s := range cfg.Services {
		if len(s.Profiles) > 0 {
			continue
		}
		n.MemoryBytes += s.MemoryBytes
		n.GPU = n.GPU || s.GPU
	}
	return n
}

// missingImages are the images the recipe builds that the engine lacks.
func (a *App) missingImages(ctx context.Context, cfg compose.Model) ([]string, error) {
	seen := map[string]bool{}
	var missing []string
	for _, s := range cfg.Services {
		if !s.Builds() || seen[s.Image] {
			continue
		}
		seen[s.Image] = true
		ok, err := a.Engine.ImageExists(ctx, s.Image)
		if err != nil {
			return nil, err
		}
		if !ok {
			missing = append(missing, s.Image)
		}
	}
	sort.Strings(missing)
	return missing, nil
}

// published maps each missing image to the verified image the catalogue
// publishes for it, by digest, when it publishes every one for this
// architecture; otherwise nil, and the recipe builds.
func published(entry catalogue.Entry, missing []string, image, arch string) map[string]string {
	pulls := map[string]string{}
	for _, local := range missing {
		img, ok := entry.Images[strings.TrimPrefix(local, image)]
		if !ok || !slices.Contains(img.Arches, arch) {
			return nil
		}
		repo, _, _ := strings.Cut(img.Ref, ":")
		pulls[local] = repo + "@" + img.Digest
	}
	return pulls
}

// pull fetches each published image by its digest and names it as the
// recipe's own, so it runs exactly what admission verified.
func (a *App) pull(ctx context.Context, pulls map[string]string, w io.Writer) (int, error) {
	for _, local := range sortedKeys(pulls) {
		if err := a.Engine.Pull(ctx, pulls[local], w); err != nil {
			return 1, err
		}
		if err := a.Engine.Tag(ctx, pulls[local], local); err != nil {
			return 1, err
		}
	}
	return 0, nil
}
