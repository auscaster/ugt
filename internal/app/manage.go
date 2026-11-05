package app

import (
	"context"
	"fmt"
	"io"
	"slices"
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
	res := FindResult{Query: query, Recipes: []catalogue.Entry{}}
	var ix catalogue.Index
	var err error
	if owner, name, perr := recipe.ParseProject(query); perr == nil {
		res.Project = strings.ToLower(owner + "/" + name)
		ix, err = a.project(ctx, owner, name)
	} else if id, parseErr := recipe.ParseID(strings.ToLower(strings.TrimSpace(query))); parseErr == nil {
		res.Project = id.Owner + "/" + id.Repo
		ix, err = a.project(ctx, id.Owner, id.Repo)
		if err != nil {
			return FindResult{}, err
		}
		for _, e := range ix {
			if e.Recipe == id.String() {
				if err := a.details(ctx, &e); err != nil {
					return FindResult{}, err
				}
				res.Recipes = append(res.Recipes, e)
				break
			}
		}
		if len(res.Recipes) == 0 {
			res.Next = []string{fmt.Sprintf("UGT has no recipe named %s. Use ugt find %s to see the project's tasks.", id, res.Project)}
		} else {
			res.Next = []string{fmt.Sprintf("Run this task with ugt run %s %s.", res.Project, id.Task)}
		}
		return res, nil
	} else {
		ix, err = a.search(ctx, strings.Fields(strings.ToLower(query)))
	}
	if err != nil {
		return FindResult{}, err
	}
	res.Recipes = append(res.Recipes, ix...)
	switch {
	case len(res.Recipes) > 0:
		res.Next = []string{"Read any limits before choosing a task with ugt find <owner>/<repo>/<task>. Match the requested result to the listed tasks, inputs, settings and provided interfaces. Run a matching recipe with ugt run <owner>/<repo> [task], with --input FILE for each file it takes. Files go in " + a.Layout.Home + ". A service returns its local address. If the project needs a different task, make a separate recipe with ugt recipe new <owner>/<repo> <task> and verify it."}
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

// details reads just the selected revision from its owning catalogue. The
// published index remains compatible with helpers that validate its v0 schema.
func (a *App) details(ctx context.Context, e *catalogue.Entry) error {
	if e.Problem != "" {
		return nil
	}
	src, err := a.openEntry(ctx, *e)
	if err != nil {
		return err
	}
	f, err := recipe.Load(src)
	if err != nil {
		return err
	}
	rev, err := recipe.Revision(src)
	if err != nil {
		return err
	}
	if f.ID.String() != e.Recipe || rev != e.Revision {
		return fmt.Errorf("%s changed during lookup; find it again for the current details", e.Recipe)
	}
	e.KnownIssues = f.Recipe.KnownIssues
	if f.Recipe.Kind == recipe.KindService {
		if err := f.NamedService(); err != nil {
			e.InstallationLimit = err.Error()
		}
	}
	return nil
}

// Installed is what UGT holds for one recipe on this machine.
type Installed struct {
	Installation  string   `json:"installation,omitempty"` // local target when this is a named copy
	Recipe        string   `json:"recipe"`
	Kind          string   `json:"kind,omitempty"` // known once it has run here
	Running       bool     `json:"running"`
	Images        []string `json:"images,omitempty"`
	Workspaces    []string `json:"workspaces,omitempty"`    // where it runs
	Verifications []string `json:"verifications,omitempty"` // workspaces its verifications left
	Volumes       []string `json:"volumes,omitempty"`       // saved data, kept until delete-data
	Commands      []string `json:"commands,omitempty"`      // the commands it put on the PATH
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
		case ledger.Command:
			in.Commands = append(in.Commands, e.Name)
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
	for target, in := range byRecipe {
		if inst, err := recipe.ParseInstallation(target); err == nil && inst.Name != "" {
			in.Installation = target
			in.Recipe = inst.ID.String()
		}
		for _, ws := range in.Workspaces {
			if sess, p, err := readSession(files, ws); err == nil {
				in.Kind = sess.Kind
				if running, err := a.Engine.Running(ctx, p); err == nil && running {
					in.Running = true
				}
			}
		}
		if id, err := recipe.ParseID(target); err == nil {
			if ok, _ := a.Disk.Exists(a.Layout.Results(id)); ok {
				in.Results = a.Layout.Results(id)
			}
		}
		out = append(out, *in)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Recipe != out[j].Recipe {
			return out[i].Recipe < out[j].Recipe
		}
		return out[i].Installation < out[j].Installation
	})
	return out, nil
}

// Change is what stop, remove or delete-data did.
type Change struct {
	Installation string   `json:"installation,omitempty"`
	Recipe       string   `json:"recipe"`
	Done         []string `json:"done"`              // each thing stopped or removed
	Kept         []string `json:"kept,omitempty"`    // what stays, and how to remove it
	Missing      bool     `json:"missing,omitempty"` // UGT holds nothing for the recipe
}

func changeFor(target string) Change {
	id, _, named := strings.Cut(target, "@")
	ch := Change{Recipe: id}
	if named {
		ch.Installation = target
	}
	return ch
}

// Stop stops a recipe's running services, keeping everything else.
func (a *App) Stop(ctx context.Context, target string) (Change, error) {
	id, live, unlock, err := a.lockHeld(target)
	if err != nil {
		return Change{}, err
	}
	defer unlock()
	ch := changeFor(id)
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
		if err != nil {
			return ch, err
		}
		if !running {
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

// Remove takes away what UGT created for a recipe: its containers, images,
// workspaces, and the commands and MCP servers it provides. Its saved data
// (named volumes) and the results delivered to the Home folder stay;
// delete-data removes the data.
func (a *App) Remove(ctx context.Context, target string) (Change, error) {
	id, live, unlock, err := a.lockHeld(target)
	if err != nil {
		return Change{}, err
	}
	defer unlock()
	ch := changeFor(id)
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
		case ledger.Secret:
			continue // settings and their keychain entries survive remove
		case ledger.Image:
			others, err := a.live()
			if err != nil {
				return ch, err
			}
			shared := slices.ContainsFunc(others, func(other ledger.Entry) bool {
				return other.Kind == ledger.Image && other.Name == e.Name && other.Recipe != id
			})
			if shared {
				ch.Kept = append(ch.Kept, "the image "+e.Name+" used by another installation")
			} else if err := a.Engine.RemoveImage(ctx, e.Name); err != nil {
				return ch, err
			} else {
				ch.Done = append(ch.Done, "removed the image "+e.Name)
			}
		case ledger.Project:
			volumes, err := a.Engine.Volumes(ctx, e.Name)
			if err != nil {
				return ch, err
			}
			if len(volumes) > 0 {
				ch.Kept = append(ch.Kept, fmt.Sprintf("its saved data (%s): ugt delete-data %s removes it", strings.Join(volumes, ", "), id))
				continue // the project's ledger entry stays, so delete-data finds its volumes
			}
		case ledger.Command, ledger.MCPServer:
			done, err := a.unprovide(ctx, e)
			if err != nil {
				return ch, err
			}
			ch.Done = append(ch.Done, done)
			continue // unprovide recorded it
		}
		if err := a.record(ledger.Entry{Kind: e.Kind, Name: e.Name, Recipe: e.Recipe, Removed: a.Now()}); err != nil {
			return ch, err
		}
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
	id, live, unlock, err := a.lockHeld(target)
	if err != nil {
		return Change{}, err
	}
	defer unlock()
	ch := changeFor(id)
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
			if err := a.record(ledger.Entry{Kind: e.Kind, Name: e.Name, Recipe: e.Recipe, Removed: a.Now()}); err != nil {
				return ch, err
			}
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
		if e.Recipe == want || (strings.HasPrefix(e.Recipe, want+"/") && !strings.Contains(e.Recipe, "@")) {
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
