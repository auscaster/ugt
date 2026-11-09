package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/usegeekthing/ugt/internal/domain/catalogue"
	"github.com/usegeekthing/ugt/internal/domain/link"
)

// Linked is the catalogue's recipes a ugt:// link names, as UGT's own page
// shows them before anything runs: the one task it names, or every task of
// its project. A link runs only recipes the catalogue admitted, whose review
// is the agreement; any web page can open one, so a recipe from anywhere else
// never runs from a link.
func (a *App) Linked(ctx context.Context, l link.Link) ([]catalogue.Entry, error) {
	found, err := a.Find(ctx, l.Project)
	if err != nil {
		return nil, err
	}
	var out []catalogue.Entry
	for _, e := range found.Recipes {
		if e.Source != catalogue.Published || (l.Task != "" && e.Recipe != l.Project+"/"+l.Task) {
			continue
		}
		if err := a.details(ctx, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		what := l.Project
		if l.Task != "" {
			what += "/" + l.Task
		}
		return nil, fmt.Errorf("the catalogue has no recipe for %s: ask your AI app to %q", what, "Use the Geek Thing: github.com/"+l.Project)
	}
	return out, nil
}

// LinkedRun runs a recipe a link named, for the user who started it on UGT's
// page, as a job the page follows: at the revision the page showed, so a
// recipe the catalogue changed meanwhile never runs in its place.
func (a *App) LinkedRun(ctx context.Context, e catalogue.Entry, instance string, inputs []string, texts, values map[string]string) (string, error) {
	if e.Source != catalogue.Published {
		return "", fmt.Errorf("%s is not a catalogue recipe: links run only those", e.Recipe)
	}
	parts := strings.SplitN(e.Recipe, "/", 3)
	if len(parts) != 3 {
		return "", fmt.Errorf("recipe %q is not owner/repo/task", e.Recipe)
	}
	opts := RunOptions{Project: parts[0] + "/" + parts[1], Task: parts[2], Instance: instance, Revision: e.Revision, Inputs: inputs, Texts: texts, Values: values}
	j := a.Start(ctx, "run", e.Recipe, func(ctx context.Context, progress func(string, string)) (any, error) {
		opts.Progress = progress
		return a.Run(ctx, opts)
	})
	return j.ID, nil
}
