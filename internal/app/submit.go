package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/recipe"
	"github.com/auscaster/ugt/internal/domain/report"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// SubmitOptions says how to share a recipe.
type SubmitOptions struct {
	// Agent is who made and verified the recipe, for the pull request: an
	// agent and model, or a person.
	Agent    string
	DryRun   bool // say what would be proposed, change nothing
	Progress func(step, message string)
}

// SubmitResult is a recipe shared, or what stopped it.
type SubmitResult struct {
	Recipe      string        `json:"recipe"`
	Revision    string        `json:"revision"`
	Report      report.Report `json:"report"`
	Reused      bool          `json:"reused_report"` // a passing verification of this revision on this machine was used
	Leaks       []recipe.Leak `json:"leaks,omitempty"`
	Files       []string      `json:"files,omitempty"`
	Title       string        `json:"title,omitempty"`
	Body        string        `json:"body,omitempty"`
	PullRequest string        `json:"pull_request,omitempty"`
	DryRun      bool          `json:"dry_run,omitempty"`
}

// ErrNotShareable is wrapped when a recipe fails verification or holds
// something personal; the result says what.
var ErrNotShareable = errors.New("the recipe cannot be shared yet")

// Submit shares a recipe with the catalogue: it verifies the recipe (or uses
// a passing verification of this exact revision on this machine), checks
// that nothing personal is in it, and opens a pull request as the user with
// the folder exactly as verified, writing nothing into it. Admission verifies
// it again on clean machines; images built here are never uploaded.
func (a *App) Submit(ctx context.Context, dir string, opts SubmitOptions) (SubmitResult, error) {
	progress := opts.Progress
	if progress == nil {
		progress = func(string, string) {}
	}
	src := a.Disk.FS(dir)
	f, err := recipe.Load(src)
	if err != nil {
		return SubmitResult{}, err
	}
	rev, err := recipe.Revision(src)
	if err != nil {
		return SubmitResult{}, err
	}
	res := SubmitResult{Recipe: f.ID.String(), Revision: rev, DryRun: opts.DryRun}

	if rep, ok := a.passingReport(f.ID, rev); ok {
		res.Report, res.Reused = rep, true
		progress("verify", "this revision passed verification here on "+rep.Started.Format("2 January"))
	} else {
		if res.Report, err = a.Verify(ctx, dir, VerifyOptions{Progress: progress}); err != nil {
			return res, err
		}
		if !res.Report.Passed {
			return res, fmt.Errorf("%w: it did not pass verification", ErrNotShareable)
		}
	}

	files := map[string][]byte{}
	if err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files[p], err = fs.ReadFile(src, p)
		return err
	}); err != nil {
		return res, err
	}
	if res.Leaks = recipe.Scrub(files, a.private(ctx, f)); len(res.Leaks) > 0 {
		return res, fmt.Errorf("%w: it holds %d personal things", ErrNotShareable, len(res.Leaks))
	}

	for _, name := range sortedKeys(files) {
		res.Files = append(res.Files, name)
	}
	update := a.inCatalogue(ctx, f.ID.String())
	res.Title, res.Body = proposal(f, res.Report, update, opts.Agent)
	if opts.DryRun {
		return res, nil
	}
	verb := "add"
	if update {
		verb = "update"
	}
	progress("submit", "opening a pull request to "+catalogue.Repo)
	res.PullRequest, err = a.GitHub.Propose(ctx, Proposal{
		Repo:    catalogue.Repo,
		Dir:     path.Join(catalogue.Dir, f.ID.Path()),
		Files:   files,
		Branch:  "recipe/" + f.ID.Owner + "-" + f.ID.Repo + "-" + f.ID.Task,
		Message: fmt.Sprintf("feat(recipes): %s %s", verb, f.ID),
		Title:   res.Title,
		Body:    res.Body,
	})
	return res, err
}

// passingReport is the report of a full passing verification of this
// revision on this machine, when there is one.
func (a *App) passingReport(id recipe.ID, rev string) (report.Report, bool) {
	file := a.Layout.Report(id, rev)
	data, err := fs.ReadFile(a.Disk.FS(filepath.Dir(file)), filepath.Base(file))
	if err != nil {
		return report.Report{}, false
	}
	var rep report.Report
	if json.Unmarshal(data, &rep) != nil || !rep.Passed || rep.Revision != rev {
		return report.Report{}, false
	}
	return rep, true
}

// private is what of this user's must not leave the machine.
func (a *App) private(ctx context.Context, f *recipe.Folder) recipe.Private {
	p := recipe.Private{
		Paths:  []string{filepath.Dir(a.Layout.Home), a.Layout.State, a.Layout.Home},
		Inputs: map[string]string{},
	}
	if a.Secrets != nil {
		for _, s := range f.Recipe.Settings {
			if s.Kind == "secret" {
				if v, ok, err := a.Secrets.Get(ctx, f.ID.String(), s.Name); err == nil && ok {
					p.Secrets = append(p.Secrets, v)
				}
			}
		}
	}
	// the files the user last ran the recipe on
	files := a.Runtime.Files()
	inputs := path.Join(a.Layout.Workspace(f.ID), workspace.InputsDir)
	if list, err := files.List(inputs); err == nil {
		for _, fi := range list {
			if data, err := files.ReadFile(fi.Path); err == nil {
				p.Inputs[sha256Hex(data)] = path.Base(fi.Path)
			}
		}
	}
	return p
}

// inCatalogue reports whether the published catalogue holds the recipe, so
// the pull request updates it rather than adding it.
func (a *App) inCatalogue(ctx context.Context, id string) bool {
	for _, c := range a.Catalogues {
		if c.Source() != catalogue.Published {
			continue
		}
		if _, err := c.Open(ctx, id); err == nil {
			return true
		}
	}
	return false
}

// proposal writes the pull request's title and description.
func proposal(f *recipe.Folder, rep report.Report, update bool, agent string) (string, string) {
	verb := "Add"
	if update {
		verb = "Update"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s, from [%s](%s) at `%s`.\n\n", f.Recipe.Task, strings.TrimPrefix(f.Recipe.Repo, "https://github.com/"), f.Recipe.Repo, f.Recipe.Commit[:12])
	fmt.Fprintf(&b, "| | |\n| --- | --- |\n| Recipe | `%s`, revision `%s` |\n| Kind | %s |\n| Packaging | %s |\n| Licence | %s |\n", f.ID, rep.Revision, f.Recipe.Kind, f.Recipe.Packaging, f.Recipe.Licence)
	if len(f.Recipe.Access) > 0 {
		b.WriteString("\n**Access beyond the sandbox:**\n\n")
		for _, a := range f.Recipe.Access {
			fmt.Fprintf(&b, "- `%s`: %s\n", a.Kind, a.Why)
		}
	}
	verified := "\n**Verified** with `ugt recipe verify` on linux/" + rep.Arch
	if agent != "" {
		verified += " by " + agent
	}
	b.WriteString(verified + ":\n\n| Step | Exit | Seconds |\n| --- | --- | --- |\n")
	for _, s := range rep.Steps {
		fmt.Fprintf(&b, "| %s | %d | %.0f |\n", s.Name, s.Exit, s.Seconds)
	}
	rejected := 0
	for _, p := range rep.Strength {
		if p.Rejected {
			rejected++
		}
	}
	fmt.Fprintf(&b, "\nCheck strength: the checks rejected %d of %d broken outputs.\n\nSubmitted with `ugt recipe submit`; admission verifies it again on clean amd64 and arm64 machines.\n", rejected, len(rep.Strength))
	return fmt.Sprintf("%s recipe %s", verb, f.ID), b.String()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
