package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"

	"github.com/auscaster/ugt/internal/domain/compose"
	"github.com/auscaster/ugt/internal/domain/diagnose"
	"github.com/auscaster/ugt/internal/domain/recipe"
	"github.com/auscaster/ugt/internal/domain/report"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// steps runs a recipe's steps and records each as a report does: its exit,
// duration, log tail and diagnosis, with the full log kept in the workspace.
type steps struct {
	files    Files
	dir      string // the workspace
	now      Clock
	progress func(step, message string)
	done     []report.Step
}

// run runs one step and reports whether it succeeded.
func (s *steps) run(name string, fn func(io.Writer) (int, error)) bool {
	s.progress(name, "started")
	t0 := s.now()
	var buf bytes.Buffer
	exit, err := fn(&buf)
	if err != nil {
		fmt.Fprintf(&buf, "\nugt: %v\n", err)
		if exit == 0 {
			exit = -1
		}
	}
	_ = s.files.WriteFile(path.Join(s.dir, ".ugt", name+".log"), buf.Bytes())
	step := report.Step{Name: name, Exit: exit, Seconds: s.now().Sub(t0).Seconds()}
	if exit != 0 {
		step.Log = tail(buf.String(), logTail)
		step.Findings = diagnose.Diagnose(exit, buf.String())
	}
	s.done = append(s.done, step)
	s.progress(name, fmt.Sprintf("exit %d", exit))
	return exit == 0
}

// prepareDirs creates a workspace's folders, sharing inputs/ and outputs/
// with whatever user the recipe's images run as.
func prepareDirs(files Files, ws string) error {
	for _, d := range []string{workspace.InputsDir, workspace.OutputsDir, ".ugt"} {
		if err := files.MkdirAll(path.Join(ws, d)); err != nil {
			return err
		}
	}
	for _, d := range []string{workspace.InputsDir, workspace.OutputsDir} {
		if err := files.Share(path.Join(ws, d)); err != nil {
			return fmt.Errorf("share %s with the recipe's services: %w", d, err)
		}
	}
	return nil
}

// stage writes a recipe folder into the runtime at dest. Every executor
// stages the same way, so file modes never travel: recipes run their scripts
// through an interpreter.
func stage(files Files, src fs.FS, dest string) error {
	if err := files.RemoveAll(dest); err != nil {
		return err
	}
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := path.Join(dest, p)
		if d.IsDir() {
			return files.MkdirAll(target)
		}
		data, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return files.WriteFile(target, data)
	})
}

// sessionFile is what a run or verification leaves in its workspace, so later
// commands (exec, verify --from, stop, remove) can step back into its project.
const sessionFile = ".ugt/session.json"

type session struct {
	Recipe   string            `json:"recipe"`
	Kind     string            `json:"kind"`
	Revision string            `json:"revision"`
	Project  string            `json:"project"`
	Env      map[string]string `json:"env"`
}

// writeSession records the project, with secret values left out: they stay
// in the system's secret store and never reach the disk.
func writeSession(files Files, p Project, r recipe.Recipe, rev string, secrets []string) error {
	env := map[string]string{}
	for k, v := range p.Env {
		env[k] = v
	}
	for _, name := range secrets {
		env[name] = ""
	}
	data, err := json.MarshalIndent(session{Recipe: r.ID, Kind: r.Kind, Revision: rev, Project: p.Name, Env: env}, "", "  ")
	if err != nil {
		return err
	}
	return files.WriteFile(path.Join(p.Dir, sessionFile), data)
}

// readSession rebuilds the project a run or verification left in ws.
func readSession(files Files, ws string) (session, Project, error) {
	data, err := files.ReadFile(path.Join(ws, sessionFile))
	if err != nil {
		return session{}, Project{}, err
	}
	var s session
	if err := json.Unmarshal(data, &s); err != nil {
		return session{}, Project{}, fmt.Errorf("%s: %w", sessionFile, err)
	}
	p := Project{
		Name:  s.Project,
		Dir:   ws,
		Files: []string{path.Join(workspace.RecipeDir, recipe.ComposeFile), compose.OverrideFile},
		Env:   s.Env,
	}
	return s, p, nil
}
