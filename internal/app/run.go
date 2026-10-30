package app

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

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
	Inputs   []string
	To       string            // where results go; Home/<owner>/<repo>/<task> when empty
	Values   map[string]string // value settings, by name
	Progress func(step, message string)
}

// RunResult is a finished run, or a service left running.
type RunResult struct {
	Recipe   string           `json:"recipe"`
	Kind     string           `json:"kind"`
	Source   catalogue.Source `json:"source"`
	Revision string           `json:"revision"`
	Passed   bool             `json:"passed"` // the task ran and its check passed
	Steps    []report.Step    `json:"steps"`
	Results  string           `json:"results,omitempty"` // where the outputs were delivered
	Files    []string         `json:"files,omitempty"`   // the delivered files, relative to Results
	URLs     []string         `json:"urls,omitempty"`    // a running service's local addresses
	Seconds  float64          `json:"seconds"`
}

// NotPlaced is a run refused because the machine cannot hold the recipe.
type NotPlaced struct{ placement.Decision }

func (e *NotPlaced) Error() string { return e.Reason + " " + e.Fix }

// Run does a recipe's task on the user's files: it finds the recipe, checks
// the machine can hold it, builds its image unless the engine has it, copies
// the inputs in, runs the task and its check, and delivers the outputs to the
// Home folder. A job's stack is then taken down; a service keeps running at
// its local addresses. A project with no recipe returns *catalogue.NoRecipe.
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
	res = RunResult{Recipe: f.ID.String(), Kind: f.Recipe.Kind, Source: entry.Source, Revision: rev}
	inputs, err := a.inputFiles(f.Recipe, opts.Inputs)
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

	files := a.Runtime.Files()
	ws := a.Layout.Workspace(f.ID)
	// an earlier run may still be up, with the workspace's folders mounted:
	// take it down, keeping its volumes, before replacing them
	if _, earlier, err := readSession(files, ws); err == nil {
		if err := a.Engine.Down(ctx, earlier, false, io.Discard); err != nil {
			return res, fmt.Errorf("stop the earlier run of %s: %w", f.ID, err)
		}
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
	env[recipe.VarImage], env[recipe.VarArch] = f.ID.Image(rev), info.Arch
	project := Project{Name: f.ID.Project(), Dir: ws, Files: []string{path.Join(workspace.RecipeDir, recipe.ComposeFile)}, Env: env}
	cfg, err := a.Engine.Config(ctx, project)
	if err != nil {
		return res, fmt.Errorf("read compose.yaml: %w", err)
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
	if err := a.writeOverride(files, ws, cfg, f.ID.String(), rev); err != nil {
		return res, err
	}
	project.Files = append(project.Files, compose.OverrideFile)
	if err := writeSession(files, project, f.Recipe, rev, secrets); err != nil {
		return res, err
	}
	_ = a.record(ledger.Entry{Kind: ledger.Workspace, Name: ws, Recipe: f.ID.String()})
	_ = a.record(ledger.Entry{Kind: ledger.Project, Name: project.Name, Recipe: f.ID.String()})

	st := &steps{files: files, dir: ws, now: a.Now, progress: progress}
	defer func() { res.Steps = st.done }()
	if missing, err := a.missingImages(ctx, cfg); err != nil {
		return res, err
	} else if len(missing) > 0 {
		if !st.run(report.StepBuild, func(w io.Writer) (int, error) { return a.Engine.Build(ctx, project, w) }) {
			return res, nil
		}
		for _, image := range missing {
			_ = a.record(ledger.Entry{Kind: ledger.Image, Name: image, Recipe: f.ID.String()})
		}
	}

	if f.Recipe.Kind == recipe.KindService {
		if !st.run(report.StepTask, func(w io.Writer) (int, error) { return a.up(ctx, project, w) }) ||
			!st.run(report.StepCheck, func(w io.Writer) (int, error) {
				return a.Engine.Run(ctx, project, recipe.ServiceCheck, nil, w)
			}) {
			return res, nil
		}
		res.URLs = a.urls(ctx, project, cfg)
		res.Passed = true
		return res, nil
	}

	defer func() {
		var discard strings.Builder
		_ = a.Engine.Down(context.WithoutCancel(ctx), project, false, &discard)
	}()
	service := func(name string) func(io.Writer) (int, error) {
		return func(w io.Writer) (int, error) { return a.Engine.Run(ctx, project, name, nil, w) }
	}
	if !st.run(report.StepTask, service(recipe.ServiceTask)) || !st.run(report.StepCheck, service(recipe.ServiceCheck)) {
		return res, nil
	}
	res.Results = opts.To
	if res.Results == "" {
		res.Results = a.Layout.Results(f.ID)
	}
	outputs := path.Join(ws, workspace.OutputsDir)
	produced, err := files.List(outputs)
	if err != nil {
		return res, err
	}
	if err := files.CopyOut(outputs, res.Results); err != nil {
		return res, fmt.Errorf("deliver the results to %s: %w", res.Results, err)
	}
	for _, p := range produced {
		res.Files = append(res.Files, strings.TrimPrefix(p.Path, outputs+"/"))
	}
	res.Passed = true
	return res, nil
}

// resolve finds a recipe across the catalogues and opens it.
func (a *App) resolve(ctx context.Context, owner, name, task string) (catalogue.Entry, fs.FS, error) {
	ix, err := a.index(ctx)
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

// index gathers every catalogue's entries, in order of precedence.
func (a *App) index(ctx context.Context) (catalogue.Index, error) {
	var sources [][]catalogue.Entry
	for _, c := range a.Catalogues {
		entries, err := c.Entries(ctx)
		if err != nil {
			return nil, fmt.Errorf("the %s catalogue: %w", c.Source(), err)
		}
		for i := range entries {
			entries[i].Here = entries[i].Problem == "" && a.passedHere(entries[i].Recipe, entries[i].Revision)
		}
		sources = append(sources, entries)
	}
	return catalogue.Merge(sources...), nil
}

// input is one user file and where it goes in the workspace.
type input struct{ path, host string }

// inputFiles maps the user's files to the inputs recipe.json declares.
func (a *App) inputFiles(r recipe.Recipe, given []string) ([]input, error) {
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
		target := ""
		for _, d := range declared {
			if d == name || path.Base(d) == name {
				target = d
			}
		}
		if target == "" {
			return nil, fmt.Errorf("%s is not one of %s's inputs; %s", name, r.ID, describe())
		}
		host, err := a.findInput(file)
		if err != nil {
			return nil, err
		}
		out, assigned[target] = append(out, input{target, host}), true
	}
	for _, d := range declared {
		if assigned[d] {
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
func needs(cfg ComposeConfig) placement.Needs {
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
func (a *App) missingImages(ctx context.Context, cfg ComposeConfig) ([]string, error) {
	seen := map[string]bool{}
	var missing []string
	for _, s := range cfg.Services {
		if !s.Builds || seen[s.Image] {
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

// urls are the local addresses of a running service's published ports.
func (a *App) urls(ctx context.Context, p Project, cfg ComposeConfig) []string {
	var out []string
	names := make([]string, 0, len(cfg.Services))
	for name := range cfg.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s := cfg.Services[name]
		if len(s.Profiles) > 0 {
			continue
		}
		for _, port := range s.Ports {
			if addr, err := a.Engine.Port(ctx, p, name, port); err == nil {
				out = append(out, "http://"+addr+"/")
			}
		}
	}
	return out
}
