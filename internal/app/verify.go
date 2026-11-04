package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/auscaster/ugt/internal/domain/access"
	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/compose"
	"github.com/auscaster/ugt/internal/domain/ledger"
	"github.com/auscaster/ugt/internal/domain/recipe"
	"github.com/auscaster/ugt/internal/domain/report"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// VerifyOptions changes how a recipe is verified.
type VerifyOptions struct {
	// Images replaces the build with images already built, keyed by suffix:
	// "" for $UGT_IMAGE, "-api" for $UGT_IMAGE-api, mapped to a reference to pull.
	Images map[string]string
	// From resumes the recipe's last verification at this step (sample, task,
	// check or sample-check) in its workspace, with its image, after
	// refreshing the recipe files. Empty verifies from scratch.
	From string
	// Progress, when set, hears each step as it starts and ends.
	Progress func(step, message string)
	// Allow is the user's agreement, given on the command line, to the access
	// beyond the sandbox the recipe declares; without it UGT asks the user.
	Allow bool
}

// logTail is how much of a failed step's output a report keeps.
const logTail = 4000

// The steps a verification runs after building, in order.
var runSteps = []string{report.StepSample, report.StepTask, report.StepCheck, report.StepSampleCheck}

// Verify runs a recipe folder the way every executor does, in a fresh scratch
// workspace: build (or pull), sample, task, check, sample-check, then the
// check-strength test. Errors mean the recipe could not be verified at all;
// a recipe that ran and failed comes back as a report with Passed false.
func (a *App) Verify(ctx context.Context, dir string, opts VerifyOptions) (rep report.Report, err error) {
	progress := opts.Progress
	if progress == nil {
		progress = func(string, string) {}
	}
	first := 0
	if opts.From != "" {
		if first = slices.Index(runSteps, opts.From); first < 0 {
			return report.Report{}, fmt.Errorf("--from %s: verification resumes at %s", opts.From, strings.Join(runSteps, ", "))
		}
		if len(opts.Images) > 0 {
			return report.Report{}, fmt.Errorf("--from reuses the last verification's image, so it takes no --pull")
		}
	}
	started := a.Now()
	sc, err := a.openScratch(ctx, dir, opts.From != "", opts.Allow)
	if err != nil {
		return report.Report{}, err
	}
	f, project, cfg, engine, files := sc.folder, sc.project, sc.config, a.Engine, a.Runtime.Files()
	if f.Recipe.Kind == recipe.KindService && first > slices.Index(runSteps, report.StepTask) {
		first = slices.Index(runSteps, report.StepTask) // a service's checks need its stack running
	}

	rep = report.Report{
		Format: report.Format, Recipe: f.ID.String(), Revision: sc.revision, Run: sc.run, Arch: project.Env[recipe.VarArch],
		Started: started, Workspace: project.Dir,
	}
	if sc.reused {
		rep.From = runSteps[first]
		rep.Image = project.Env[recipe.VarImage]
		progress("resume", fmt.Sprintf("from %s, with the image of the last verification (%s)", rep.From, rep.Image))
	}
	// Named results: the deferred cleanup's duration reaches the caller.
	defer func() {
		var discard bytes.Buffer
		_ = engine.Down(context.WithoutCancel(ctx), project, true, &discard)
		rep.Seconds = a.Now().Sub(started).Seconds()
	}()

	st := &steps{files: files, dir: project.Dir, now: a.Now, progress: progress}
	run := st.run
	defer func() { rep.Steps = st.done }()

	if builds(cfg) && !sc.reused {
		if len(opts.Images) > 0 {
			if !run(report.StepPull, func(w io.Writer) (int, error) {
				for suffix, ref := range opts.Images {
					if err := engine.Pull(ctx, ref, w, st.pullProgress); err != nil {
						return 1, err
					}
					if err := engine.Tag(ctx, ref, project.Env[recipe.VarImage]+suffix); err != nil {
						return 1, err
					}
					_ = a.record(ledger.Entry{Kind: ledger.Image, Name: project.Env[recipe.VarImage] + suffix, Recipe: f.ID.String()})
				}
				return 0, nil
			}) {
				return rep, nil
			}
		} else {
			if !run(report.StepBuild, func(w io.Writer) (int, error) { return engine.Build(ctx, project, w) }) {
				return rep, nil
			}
			_ = a.record(ledger.Entry{Kind: ledger.Image, Name: project.Env[recipe.VarImage], Recipe: f.ID.String()})
		}
	}
	service := func(name string) func(io.Writer) (int, error) {
		return func(w io.Writer) (int, error) { return a.runService(ctx, project, name, w) }
	}
	steps := map[string]func(io.Writer) (int, error){
		report.StepSample:      service(recipe.ServiceSample),
		report.StepTask:        service(recipe.ServiceTask),
		report.StepCheck:       service(recipe.ServiceCheck),
		report.StepSampleCheck: service(recipe.ServiceSampleCheck),
	}
	if _, ok := cfg.Services[recipe.ServiceTask]; !ok {
		steps[report.StepTask] = func(w io.Writer) (int, error) { return a.up(ctx, project, w) }
	}
	for _, name := range runSteps[first:] {
		if !run(name, steps[name]) {
			return rep, nil
		}
	}

	progress(report.StepStrength, "started")
	probes, err := a.checkStrength(ctx, files, engine, project, f.Recipe.Kind)
	if err != nil {
		return rep, err
	}
	rep.Strength = probes
	rep.Passed = len(rep.WeakChecks()) == 0
	progress(report.StepStrength, fmt.Sprintf("%d probes, %d weak", len(probes), len(rep.WeakChecks())))
	if rep.Passed && rep.From == "" {
		rep.Steps = st.done
		if data, err := json.MarshalIndent(rep, "", "  "); err == nil {
			_ = a.Disk.WriteFile(a.Layout.Report(f.ID, sc.run), data)
		}
	}
	return rep, nil
}

// passedHere reports whether a recipe's run revision passed a full
// verification on this machine.
func (a *App) passedHere(id, run string) bool {
	parsed, err := recipe.ParseID(id)
	if err != nil {
		return false
	}
	ok, _ := a.Disk.Exists(a.Layout.Report(parsed, run))
	return ok
}

// up starts a project's stack. When it fails, the log gains what each
// container logged, which is where the reason shows.
func (a *App) up(ctx context.Context, p Project, log io.Writer) (int, error) {
	exit, err := a.Engine.Up(ctx, p, log)
	if exit != 0 || err != nil {
		fmt.Fprintln(log, "\nugt: the stack did not start; what its containers logged:")
		_ = a.Engine.Logs(context.WithoutCancel(ctx), p, log)
	}
	return exit, err
}

// failedDependency is Compose's word on a container a service needs that did
// not start, before the service itself runs.
var failedDependency = regexp.MustCompile(`dependency failed to start: container (\S+) (?:is unhealthy|exited)`)

// runService runs one of a project's services to its end. When a service it
// needs did not start, the log gains what that one logged, which is where the
// reason shows.
func (a *App) runService(ctx context.Context, p Project, service string, log io.Writer) (int, error) {
	start := &head{n: 16 << 10} // Compose says it before the service writes anything
	exit, err := a.Engine.Run(ctx, p, service, nil, io.MultiWriter(log, start))
	if m := failedDependency.FindSubmatch(start.buf); exit != 0 && m != nil {
		fmt.Fprintf(log, "\nugt: %s did not start; what it logged:\n", m[1])
		_ = a.Engine.Logs(context.WithoutCancel(ctx), p, log, serviceOf(p.Name, string(m[1]))...)
	}
	return exit, err
}

// serviceOf is the service a project's container runs, by Compose's names,
// project-service-n; none for a container named its own way, so that every
// container's log is read.
func serviceOf(project, container string) []string {
	rest, ok := strings.CutPrefix(container, project+"-")
	i := strings.LastIndexByte(rest, '-')
	if !ok || i <= 0 {
		return nil
	}
	return []string{rest[:i]}
}

// head keeps the first n bytes written to it.
type head struct {
	buf []byte
	n   int
}

func (h *head) Write(p []byte) (int, error) {
	if room := h.n - len(h.buf); room > 0 {
		h.buf = append(h.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

// scratch is a recipe's project in a scratch workspace, ready to run.
type scratch struct {
	folder   *recipe.Folder
	revision string
	run      string // its run revision, which names its images
	project  Project
	config   compose.Model
	reused   bool // the recipe's last verification workspace, not a fresh one
}

// openScratch loads a recipe folder and prepares its scratch workspace: a
// fresh one, or with reuse the recipe's last one, keeping its inputs, outputs
// and image and refreshing the recipe files. Either way the recipe's access is
// checked, and agreed to by the user (with allow, on the command line), before
// anything runs, and the helper's Compose override is written for the recipe
// as it is now.
func (a *App) openScratch(ctx context.Context, dir string, reuse, allow bool) (*scratch, error) {
	src := a.Disk.FS(dir)
	f, err := recipe.Load(src)
	if err != nil {
		return nil, err
	}
	rev, err := recipe.Revision(src)
	if err != nil {
		return nil, err
	}
	run, err := recipe.RunRevision(src)
	if err != nil {
		return nil, err
	}
	status, err := a.Runtime.Status(ctx)
	if err != nil {
		return nil, err
	}
	if !status.Ready {
		return nil, fmt.Errorf("the engine is not ready: %s %s", status.Problem, status.Fix)
	}
	files := a.Runtime.Files()
	sc := &scratch{folder: f, revision: rev, run: run, reused: reuse}
	composeFile := path.Join(workspace.RecipeDir, recipe.ComposeFile)
	info, err := a.Engine.Info(ctx)
	if err != nil {
		return nil, err
	}
	if reuse {
		ws, err := a.lastScratch(f.ID)
		if err != nil {
			return nil, err
		}
		_, p, err := readSession(files, ws)
		if err != nil {
			return nil, fmt.Errorf("the last verification of %s left no session in %s (%v): run ugt recipe verify without --from", f.ID, ws, err)
		}
		if err := stage(files, src, path.Join(ws, workspace.RecipeDir)); err != nil {
			return nil, err
		}
		p.Files = []string{composeFile}
		sc.project = p
	} else {
		ws := a.Layout.Scratch(f.ID, a.Now())
		if err := prepareDirs(files, ws); err != nil {
			return nil, err
		}
		if err := stage(files, src, path.Join(ws, workspace.RecipeDir)); err != nil {
			return nil, err
		}
		sc.project = Project{
			Name:  f.ID.Project() + "-verify",
			Dir:   ws,
			Files: []string{composeFile},
			Env:   map[string]string{recipe.VarImage: f.ID.Image(run), recipe.VarArch: info.Arch},
		}
		if err := writeSession(files, sc.project, f.Recipe, rev, nil, nil); err != nil {
			return nil, err
		}
		_ = a.record(ledger.Entry{Kind: ledger.Workspace, Name: ws, Recipe: f.ID.String()})
	}
	cfg, err := a.Engine.Config(ctx, sc.project)
	if err != nil {
		return nil, fmt.Errorf("read compose.yaml: %w", err)
	}
	if err := checkAccess(f, cfg, sc.project.Dir); err != nil {
		return nil, err
	}
	if err := a.consent(ctx, f, catalogue.Local, access.Grants(cfg, sc.project.Dir), allow); err != nil {
		return nil, err
	}
	if err := a.writeOverride(files, sc.project.Dir, cfg, f.ID.String(), run, info.CPUs, nil); err != nil {
		return nil, err
	}
	sc.project.Files = append(sc.project.Files, compose.OverrideFile)
	sc.config = cfg
	return sc, nil
}

// lastScratch is the newest scratch workspace the ledger holds for a recipe.
func (a *App) lastScratch(id recipe.ID) (string, error) {
	var entries []ledger.Entry
	if a.Ledger != nil {
		all, err := a.Ledger.Entries()
		if err != nil {
			return "", err
		}
		entries = ledger.Live(all)
	}
	var last ledger.Entry
	for _, e := range entries {
		if e.Kind == ledger.Workspace && e.Recipe == id.String() && strings.Contains(e.Name, "/.scratch/") && !e.Created.Before(last.Created) {
			last = e
		}
	}
	if last.Name == "" {
		return "", fmt.Errorf("%s has not been verified on this machine yet: run ugt recipe verify first", id)
	}
	return last.Name, nil
}

// checkAccess refuses a recipe whose resolved model grants access it does not
// declare, before anything is built or run.
func checkAccess(f *recipe.Folder, m compose.Model, ws string) error {
	if problems := access.Check(f.Recipe.Access, m, ws); len(problems) > 0 {
		return fmt.Errorf("%s reaches beyond the sandbox in ways recipe.json does not declare:\n- %s", f.ID, strings.Join(problems, "\n- "))
	}
	return nil
}

// writeOverride writes the helper's Compose override into the workspace, for
// a recipe at a run revision and an engine with cpus CPUs (0 when unknown),
// publishing what publish names.
func (a *App) writeOverride(files Files, ws string, m compose.Model, id, run string, cpus int, publish []compose.Publish) error {
	data, err := compose.Override(m, id, run, cpus, publish)
	if err != nil {
		return err
	}
	return files.WriteFile(path.Join(ws, compose.OverrideFile), data)
}

// Mutations the check-strength test applies to a verified run's outputs.
// Every check must reject every one of them.
const (
	MutationMissing     = "missing"      // outputs/ is empty
	MutationEmpty       = "empty"        // every output file has no bytes
	MutationGarbage     = "garbage"      // every output file holds the same number of meaningless bytes
	MutationServiceDown = "service-down" // a service recipe's stack is stopped
)

// checkStrength runs check and sample-check against deliberately broken
// outputs and reports whether each was rejected. Outputs are restored after.
func (a *App) checkStrength(ctx context.Context, files Files, engine Engine, p Project, kind string) ([]report.Probe, error) {
	outputs := path.Join(p.Dir, workspace.OutputsDir)
	pristine := path.Join(p.Dir, ".ugt", "outputs-verified")
	if err := files.RemoveAll(pristine); err != nil {
		return nil, err
	}
	if err := files.Copy(outputs, pristine); err != nil {
		return nil, err
	}
	produced, err := files.List(pristine)
	if err != nil {
		return nil, err
	}
	restore := func() error {
		if err := files.RemoveAll(outputs); err != nil {
			return err
		}
		if err := files.Copy(pristine, outputs); err != nil {
			return err
		}
		return files.Share(outputs)
	}
	type mutation struct {
		name  string
		apply func() error
	}
	var mutations []mutation
	if len(produced) > 0 {
		mutations = append(mutations,
			mutation{MutationMissing, func() error {
				if err := files.RemoveAll(outputs); err != nil {
					return err
				}
				if err := files.MkdirAll(outputs); err != nil {
					return err
				}
				return files.Share(outputs)
			}},
			mutation{MutationEmpty, func() error {
				return rewrite(files, pristine, outputs, produced, func(int64, string) []byte { return nil })
			}},
			mutation{MutationGarbage, func() error { return rewrite(files, pristine, outputs, produced, garbage) }},
		)
	}
	if kind == recipe.KindService {
		mutations = append(mutations, mutation{MutationServiceDown, func() error {
			var discard bytes.Buffer
			return engine.Stop(ctx, p, &discard)
		}})
	}

	var probes []report.Probe
	for _, m := range mutations {
		if err := restore(); err != nil {
			return nil, err
		}
		if err := m.apply(); err != nil {
			return nil, fmt.Errorf("apply %s: %w", m.name, err)
		}
		for _, check := range []string{recipe.ServiceCheck, recipe.ServiceSampleCheck} {
			var discard bytes.Buffer
			exit, err := engine.Run(ctx, p, check, nil, &discard)
			if err != nil {
				return nil, fmt.Errorf("run %s against %s outputs: %w", check, m.name, err)
			}
			probes = append(probes, report.Probe{Mutation: m.name, Check: check, Rejected: exit != 0})
		}
	}
	return probes, restore()
}

// rewrite replaces each produced output file with content(size, path).
func rewrite(files Files, pristine, outputs string, produced []FileInfo, content func(int64, string) []byte) error {
	for _, fi := range produced {
		rel := strings.TrimPrefix(fi.Path, pristine+"/")
		if err := files.WriteFile(path.Join(outputs, rel), content(fi.Size, rel)); err != nil {
			return err
		}
	}
	return nil
}

// garbage is size deterministic, meaningless bytes for a file, at least 64.
func garbage(size int64, name string) []byte {
	if size < 64 {
		size = 64
	}
	out := make([]byte, 0, size)
	seed := sha256.Sum256([]byte(name))
	for int64(len(out)) < size {
		seed = sha256.Sum256(seed[:])
		out = append(out, seed[:]...)
	}
	return out[:size]
}

func builds(m compose.Model) bool {
	for _, s := range m.Services {
		if s.Builds() {
			return true
		}
	}
	return false
}

// tail returns the last n bytes of s, starting at a line boundary.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s
}
