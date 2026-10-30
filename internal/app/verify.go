package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/auscaster/ugt/internal/domain/compose"
	"github.com/auscaster/ugt/internal/domain/diagnose"
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
	// Progress, when set, hears each step as it starts and ends.
	Progress func(step, message string)
}

// logTail is how much of a failed step's output a report keeps.
const logTail = 4000

// Verify runs a recipe folder the way every executor does, in a fresh scratch
// workspace: build (or pull), sample, task, check, sample-check, then the
// check-strength test. Errors mean the recipe could not be verified at all;
// a recipe that ran and failed comes back as a report with Passed false.
func (a *App) Verify(ctx context.Context, dir string, opts VerifyOptions) (rep report.Report, err error) {
	progress := opts.Progress
	if progress == nil {
		progress = func(string, string) {}
	}
	src := a.Disk.FS(dir)
	f, err := recipe.Load(src)
	if err != nil {
		return report.Report{}, err
	}
	rev, err := recipe.Revision(src)
	if err != nil {
		return report.Report{}, err
	}
	status, err := a.Runtime.Status(ctx)
	if err != nil {
		return report.Report{}, err
	}
	if !status.Ready {
		return report.Report{}, fmt.Errorf("the engine is not ready: %s %s", status.Problem, status.Fix)
	}
	info, err := a.Engine.Info(ctx)
	if err != nil {
		return report.Report{}, err
	}

	started := a.Now()
	ws := a.Layout.Scratch(f.ID, started)
	files, engine := a.Runtime.Files(), a.Engine
	for _, d := range []string{workspace.InputsDir, workspace.OutputsDir, ".ugt"} {
		if err := files.MkdirAll(path.Join(ws, d)); err != nil {
			return report.Report{}, err
		}
	}
	if err := files.CopyIn(dir, path.Join(ws, workspace.RecipeDir)); err != nil {
		return report.Report{}, err
	}
	project := Project{
		Name:  f.ID.Project() + "-verify",
		Dir:   ws,
		Files: []string{path.Join(workspace.RecipeDir, recipe.ComposeFile)},
		Env:   map[string]string{recipe.VarImage: f.ID.Image(rev), recipe.VarArch: info.Arch},
	}
	cfg, err := engine.Config(ctx, project)
	if err != nil {
		return report.Report{}, fmt.Errorf("read compose.yaml: %w", err)
	}
	if err := a.writeOverride(files, ws, cfg, f.ID.String(), rev); err != nil {
		return report.Report{}, err
	}
	project.Files = append(project.Files, compose.OverrideFile)
	_ = a.record(ledger.Entry{Kind: ledger.Workspace, Name: ws, Recipe: f.ID.String()})

	rep = report.Report{
		Format: report.Format, Recipe: f.ID.String(), Revision: rev, Arch: info.Arch,
		Started: started, Workspace: ws,
	}
	// Named results: the deferred cleanup's duration reaches the caller.
	defer func() {
		var discard bytes.Buffer
		_ = engine.Down(context.WithoutCancel(ctx), project, true, &discard)
		rep.Seconds = a.Now().Sub(started).Seconds()
	}()

	run := func(name string, fn func(io.Writer) (int, error)) bool {
		progress(name, "started")
		t0 := a.Now()
		var buf bytes.Buffer
		exit, err := fn(&buf)
		if err != nil {
			fmt.Fprintf(&buf, "\nugt: %v\n", err)
			if exit == 0 {
				exit = -1
			}
		}
		_ = files.WriteFile(path.Join(ws, ".ugt", name+".log"), buf.Bytes())
		step := report.Step{Name: name, Exit: exit, Seconds: a.Now().Sub(t0).Seconds()}
		if exit != 0 {
			step.Log = tail(buf.String(), logTail)
			step.Findings = diagnose.Diagnose(exit, buf.String())
		}
		rep.Steps = append(rep.Steps, step)
		progress(name, fmt.Sprintf("exit %d", exit))
		return exit == 0
	}

	if builds(cfg) {
		if len(opts.Images) > 0 {
			if !run(report.StepPull, func(w io.Writer) (int, error) {
				for suffix, ref := range opts.Images {
					if err := engine.Pull(ctx, ref, w); err != nil {
						return 1, err
					}
					if err := engine.Tag(ctx, ref, project.Env[recipe.VarImage]+suffix); err != nil {
						return 1, err
					}
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
		return func(w io.Writer) (int, error) { return engine.Run(ctx, project, name, w) }
	}
	if !run(report.StepSample, service(recipe.ServiceSample)) {
		return rep, nil
	}
	task := service(recipe.ServiceTask)
	if _, ok := cfg.Services[recipe.ServiceTask]; !ok {
		task = func(w io.Writer) (int, error) { return engine.Up(ctx, project, w) }
	}
	if !run(report.StepTask, task) ||
		!run(report.StepCheck, service(recipe.ServiceCheck)) ||
		!run(report.StepSampleCheck, service(recipe.ServiceSampleCheck)) {
		return rep, nil
	}

	progress(report.StepStrength, "started")
	probes, err := a.checkStrength(ctx, files, engine, project, f.Recipe.Kind)
	if err != nil {
		return rep, err
	}
	rep.Strength = probes
	rep.Passed = len(rep.WeakChecks()) == 0
	progress(report.StepStrength, fmt.Sprintf("%d probes, %d weak", len(probes), len(rep.WeakChecks())))
	return rep, nil
}

// writeOverride writes the helper's Compose override into the workspace.
func (a *App) writeOverride(files Files, ws string, cfg ComposeConfig, id, rev string) error {
	var services []compose.Service
	for name, s := range cfg.Services {
		services = append(services, compose.Service{Name: name, Builds: s.Builds, SecurityOpt: s.SecurityOpt})
	}
	data, err := compose.Override(services, id, rev)
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
		return files.Copy(pristine, outputs)
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
				return files.MkdirAll(outputs)
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
			exit, err := engine.Run(ctx, p, check, &discard)
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

func builds(cfg ComposeConfig) bool {
	for _, s := range cfg.Services {
		if s.Builds {
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
