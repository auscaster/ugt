package app

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/auscaster/ugt/internal/domain/access"
	"github.com/auscaster/ugt/internal/domain/compose"
	"github.com/auscaster/ugt/internal/domain/recipe"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// NeedsAgreement is a run or verification stopped because the user has not
// agreed to the recipe's access and UGT cannot ask them where it runs.
type NeedsAgreement struct {
	Recipe string
	Grants []access.Grant
}

func (e *NeedsAgreement) Error() string {
	return fmt.Sprintf("%s needs the user's agreement to its access (%s), and UGT cannot ask them here: the user runs `ugt allow %s` in a terminal",
		e.Recipe, kindList(e.Grants), e.Recipe)
}

// ErrDeclined is wrapped when the user does not allow a recipe's access.
var ErrDeclined = errors.New("the user did not allow the access")

// consent makes sure the user agreed to exactly these grants for the
// recipe: once, and again whenever they change. allow is the agreement given
// on the command line; otherwise UGT asks the user itself, never through an
// agent.
func (a *App) consent(ctx context.Context, f *recipe.Folder, grants []access.Grant, allow bool) error {
	if len(grants) == 0 {
		return nil
	}
	id, digest := f.ID.String(), access.Digest(grants)
	if a.Consents != nil {
		if ok, err := a.Consents.Agreed(id, digest); err != nil || ok {
			return err
		}
	}
	if !allow {
		if a.Prompt == nil {
			return &NeedsAgreement{Recipe: id, Grants: grants}
		}
		ok, err := a.Prompt.Confirm(ctx, Question{Title: "UGT", Message: access.Describe(id, f.Recipe.Access, grants)})
		switch {
		case errors.Is(err, ErrNoPrompt):
			return &NeedsAgreement{Recipe: id, Grants: grants}
		case err != nil:
			return err
		case !ok:
			return fmt.Errorf("%w %s asks for (%s); nothing ran", ErrDeclined, id, kindList(grants))
		}
	}
	if a.Consents == nil {
		return nil
	}
	return a.Consents.Agree(id, digest)
}

// runServices are the services a run starts: the default ones and check;
// the sample's run only in verification.
func runServices(m compose.Model) []string {
	var out []string
	for name, s := range m.Services {
		if len(s.Profiles) == 0 || name == recipe.ServiceCheck {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// AllowResult is the access a recipe's runs need, and whether the user
// agreed to it.
type AllowResult struct {
	Recipe  string         `json:"recipe"`
	Grants  []access.Grant `json:"grants"`
	Allowed bool           `json:"allowed"`
}

// Allow asks the user, ahead of a run, to agree to the access a recipe's runs
// need: for an AI app on a machine where UGT cannot ask during the run. yes
// is the agreement given on the command line. The recipe is staged apart
// from its workspace, which may be running, and only read.
func (a *App) Allow(ctx context.Context, target string, yes bool) (AllowResult, error) {
	src, err := a.resolveTarget(ctx, target)
	if err != nil {
		return AllowResult{}, err
	}
	f, err := recipe.Load(src)
	if err != nil {
		return AllowResult{}, err
	}
	res := AllowResult{Recipe: f.ID.String()}
	rev, err := recipe.Revision(src)
	if err != nil {
		return res, err
	}
	env, err := a.valuesEnv(f.Recipe)
	if err != nil {
		return res, err
	}
	info, err := a.Engine.Info(ctx)
	if err != nil {
		return res, err
	}
	files := a.Runtime.Files()
	probe := a.Layout.Probe(f.ID)
	defer func() { _ = files.RemoveAll(probe) }()
	if err := stage(files, src, path.Join(probe, workspace.RecipeDir)); err != nil {
		return res, err
	}
	env[recipe.VarImage], env[recipe.VarArch] = f.ID.Image(rev), info.Arch
	m, err := a.Engine.Config(ctx, Project{Name: f.ID.Project() + "-probe", Dir: probe, Files: []string{path.Join(workspace.RecipeDir, recipe.ComposeFile)}, Env: env})
	if err != nil {
		return res, fmt.Errorf("read compose.yaml: %w", err)
	}
	if err := checkAccess(f, m, probe); err != nil {
		return res, err
	}
	res.Grants = access.In(access.Grants(m, probe), runServices(m))
	if err := a.consent(ctx, f, res.Grants, yes); err != nil {
		return res, err
	}
	res.Allowed = true
	return res, nil
}

func kindList(grants []access.Grant) string {
	kinds := access.KindsOf(grants)
	out := make([]string, len(kinds))
	for i, k := range kinds {
		out[i] = string(k)
	}
	return strings.Join(out, ", ")
}
