package app

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/ledger"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

// FindResult answers ugt find: the recipes for a project, or for some words.
type FindResult struct {
	Query   string            `json:"query"`
	Project string            `json:"project,omitempty"` // owner/repo, when the query named one
	Recipes []catalogue.Entry `json:"recipes"`
	Next    []string          `json:"next"`
}

// Find looks a GitHub project, or words describing a task, up in the
// catalogues. Finding nothing is a normal answer: it comes back with the call
// that makes a recipe.
func (a *App) Find(ctx context.Context, query string) (FindResult, error) {
	ix, err := a.index(ctx)
	if err != nil {
		return FindResult{}, err
	}
	res := FindResult{Query: query, Recipes: []catalogue.Entry{}}
	if owner, name, err := recipe.ParseProject(query); err == nil {
		res.Project = strings.ToLower(owner + "/" + name)
		res.Recipes = append(res.Recipes, ix.Project(owner, name)...)
	} else {
		res.Recipes = append(res.Recipes, ix.Search(query)...)
	}
	switch {
	case len(res.Recipes) > 0:
		res.Next = []string{"Run one with ugt run <owner>/<repo> [task] --input FILE: it builds what it needs, runs the task and its check, and puts the results in " + a.Layout.Home + "."}
	case res.Project != "":
		res.Next = []string{
			fmt.Sprintf("No recipe for %s yet, which is normal: make one with ugt recipe new %s <task>.", res.Project, res.Project),
			"Then run ugt recipe verify on it until it passes, and run it with ugt run.",
		}
	default:
		res.Next = []string{"No recipe matches. Give a GitHub project as owner/repo to find or make one."}
	}
	return res, nil
}

// Installed is what UGT holds for one recipe on this machine.
type Installed struct {
	Recipe        string   `json:"recipe"`
	Kind          string   `json:"kind,omitempty"` // known once it has run here
	Running       bool     `json:"running"`
	Images        []string `json:"images,omitempty"`
	Workspaces    []string `json:"workspaces,omitempty"`    // where it runs
	Verifications []string `json:"verifications,omitempty"` // workspaces its verifications left
	Volumes       []string `json:"volumes,omitempty"`       // saved data, kept until delete-data
	Results       string   `json:"results,omitempty"`       // the Home folder its results went to
}

// List reports everything UGT created, by recipe, from the ledger.
func (a *App) List(ctx context.Context) ([]Installed, error) {
	live, err := a.live()
	if err != nil {
		return nil, err
	}
	byRecipe := map[string]*Installed{}
	for _, e := range live {
		if e.Recipe == "" {
			continue
		}
		in := byRecipe[e.Recipe]
		if in == nil {
			in = &Installed{Recipe: e.Recipe}
			byRecipe[e.Recipe] = in
		}
		switch e.Kind {
		case ledger.Image:
			in.Images = append(in.Images, e.Name)
		case ledger.Workspace:
			if strings.Contains(e.Name, "/.scratch/") {
				in.Verifications = append(in.Verifications, e.Name)
			} else {
				in.Workspaces = append(in.Workspaces, e.Name)
			}
		case ledger.Project:
			volumes, err := a.Engine.Volumes(ctx, e.Name)
			if err != nil {
				return nil, err
			}
			in.Volumes = append(in.Volumes, volumes...)
		}
	}
	files := a.Runtime.Files()
	var out []Installed
	for _, in := range byRecipe {
		for _, ws := range in.Workspaces {
			if sess, p, err := readSession(files, ws); err == nil {
				in.Kind = sess.Kind
				if running, err := a.Engine.Running(ctx, p); err == nil && running {
					in.Running = true
				}
			}
		}
		if id, err := recipe.ParseID(in.Recipe); err == nil {
			if ok, _ := a.Disk.Exists(a.Layout.Results(id)); ok {
				in.Results = a.Layout.Results(id)
			}
		}
		out = append(out, *in)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Recipe < out[j].Recipe })
	return out, nil
}

// Change is what stop, remove or delete-data did.
type Change struct {
	Recipe  string   `json:"recipe"`
	Done    []string `json:"done"`              // each thing stopped or removed
	Kept    []string `json:"kept,omitempty"`    // what stays, and how to remove it
	Missing bool     `json:"missing,omitempty"` // UGT holds nothing for the recipe
}

// Stop stops a recipe's running services, keeping everything else.
func (a *App) Stop(ctx context.Context, target string) (Change, error) {
	id, live, err := a.held(target)
	if err != nil {
		return Change{}, err
	}
	ch := Change{Recipe: id}
	files := a.Runtime.Files()
	for _, e := range live {
		if e.Kind != ledger.Workspace {
			continue
		}
		_, p, err := readSession(files, e.Name)
		if err != nil {
			continue
		}
		running, err := a.Engine.Running(ctx, p)
		if err != nil || !running {
			continue
		}
		if err := a.Engine.Stop(ctx, p, io.Discard); err != nil {
			return ch, err
		}
		ch.Done = append(ch.Done, "stopped "+p.Name)
	}
	if len(ch.Done) == 0 {
		ch.Done = []string{"nothing was running"}
	}
	return ch, nil
}

// Remove takes away what UGT created for a recipe: its containers, images and
// workspaces. Its saved data (named volumes) and the results delivered to the
// Home folder stay; delete-data removes the data.
func (a *App) Remove(ctx context.Context, target string) (Change, error) {
	id, live, err := a.held(target)
	if err != nil {
		return Change{}, err
	}
	ch := Change{Recipe: id}
	files := a.Runtime.Files()
	verifications := 0
	for _, e := range live {
		switch e.Kind {
		case ledger.Workspace:
			if _, p, err := readSession(files, e.Name); err == nil {
				if err := a.Engine.Down(ctx, p, false, io.Discard); err != nil {
					return ch, err
				}
			}
			if err := files.RemoveAll(e.Name); err != nil {
				return ch, err
			}
			if strings.Contains(e.Name, "/.scratch/") {
				verifications++
			} else {
				ch.Done = append(ch.Done, "removed its workspace "+e.Name)
			}
		case ledger.Image:
			if err := a.Engine.RemoveImage(ctx, e.Name); err != nil {
				return ch, err
			}
			ch.Done = append(ch.Done, "removed the image "+e.Name)
		case ledger.Project:
			volumes, err := a.Engine.Volumes(ctx, e.Name)
			if err != nil {
				return ch, err
			}
			if len(volumes) > 0 {
				ch.Kept = append(ch.Kept, fmt.Sprintf("its saved data (%s): ugt delete-data %s removes it", strings.Join(volumes, ", "), id))
				continue // the project's ledger entry stays, so delete-data finds its volumes
			}
		}
		_ = a.record(ledger.Entry{Kind: e.Kind, Name: e.Name, Recipe: e.Recipe, Removed: a.Now()})
	}
	if verifications > 0 {
		ch.Done = append(ch.Done, fmt.Sprintf("removed the workspaces of %d verifications", verifications))
	}
	if parsed, err := recipe.ParseID(id); err == nil {
		if ok, _ := a.Disk.Exists(a.Layout.Results(parsed)); ok {
			ch.Kept = append(ch.Kept, "its results in "+a.Layout.Results(parsed))
		}
		if ok, _ := a.Disk.Exists(a.Layout.LocalRecipe(parsed)); ok {
			ch.Kept = append(ch.Kept, "the recipe itself, in "+a.Layout.LocalRecipe(parsed))
		}
	}
	return ch, nil
}

// DeleteData removes a recipe's saved data: the named volumes its projects
// created. Its containers are taken down first; images and workspaces stay.
func (a *App) DeleteData(ctx context.Context, target string) (Change, error) {
	id, live, err := a.held(target)
	if err != nil {
		return Change{}, err
	}
	ch := Change{Recipe: id}
	files := a.Runtime.Files()
	for _, e := range live {
		if e.Kind == ledger.Workspace {
			if _, p, err := readSession(files, e.Name); err == nil {
				if err := a.Engine.Down(ctx, p, false, io.Discard); err != nil {
					return ch, err
				}
			}
		}
	}
	for _, e := range live {
		if e.Kind != ledger.Project {
			continue
		}
		volumes, err := a.Engine.Volumes(ctx, e.Name)
		if err != nil {
			return ch, err
		}
		for _, v := range volumes {
			if err := a.Engine.RemoveVolume(ctx, v); err != nil {
				return ch, err
			}
			ch.Done = append(ch.Done, "deleted the volume "+v)
		}
		if !a.holds(live, ledger.Workspace, e.Recipe) {
			_ = a.record(ledger.Entry{Kind: e.Kind, Name: e.Name, Recipe: e.Recipe, Removed: a.Now()})
		}
	}
	if len(ch.Done) == 0 {
		ch.Done = []string{"it had no saved data"}
	}
	return ch, nil
}

// held resolves target (owner/repo/task, or owner/repo when UGT holds one
// recipe of it) to the live ledger entries of that recipe.
func (a *App) held(target string) (string, []ledger.Entry, error) {
	live, err := a.live()
	if err != nil {
		return "", nil, err
	}
	want := strings.ToLower(strings.TrimSpace(target))
	if owner, name, err := recipe.ParseProject(want); err == nil {
		want = strings.ToLower(owner + "/" + name)
	}
	ids := map[string]bool{}
	for _, e := range live {
		if e.Recipe == want || strings.HasPrefix(e.Recipe, want+"/") {
			ids[e.Recipe] = true
		}
	}
	switch len(ids) {
	case 0:
		return want, nil, fmt.Errorf("UGT holds nothing for %s on this machine; ugt list shows what it holds", target)
	case 1:
	default:
		var names []string
		for id := range ids {
			names = append(names, id)
		}
		sort.Strings(names)
		return "", nil, fmt.Errorf("UGT holds several recipes of %s; name one: %s", target, strings.Join(names, ", "))
	}
	var id string
	for k := range ids {
		id = k
	}
	var out []ledger.Entry
	for _, e := range live {
		if e.Recipe == id {
			out = append(out, e)
		}
	}
	return id, out, nil
}

func (a *App) holds(live []ledger.Entry, kind ledger.Kind, recipeID string) bool {
	for _, e := range live {
		if e.Kind == kind && e.Recipe == recipeID {
			return true
		}
	}
	return false
}

func (a *App) live() ([]ledger.Entry, error) {
	if a.Ledger == nil {
		return nil, nil
	}
	all, err := a.Ledger.Entries()
	if err != nil {
		return nil, err
	}
	return ledger.Live(all), nil
}
