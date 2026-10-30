// Package catalogue is the set of recipes UGT can run, gathered from its
// sources: recipes made on this machine and the published catalogue, read
// live from its repository, so a recipe needs no release of ugt. Sources are
// asked per project, so a catalogue of any size costs the same to use. It
// resolves a project and task to one recipe; a project with no recipe is a
// normal answer, not an error.
package catalogue

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/auscaster/ugt/internal/domain/recipe"
)

// Where the published catalogue lives: recipes are read from it live and
// proposed to it by pull request. This is the one place that names it.
const (
	Repo   = "auscaster/ugt"
	Dir    = "recipes"
	Branch = "main"
)

// Source says where an entry comes from.
type Source string

const (
	Local     Source = "local"     // made on this machine, in the local catalogue
	Published Source = "catalogue" // the published catalogue
)

// Entry is one recipe a source holds.
type Entry struct {
	Recipe   string `json:"recipe"` // owner/repo/task
	What     string `json:"what"`
	Kind     string `json:"kind"`
	Repo     string `json:"repo"`
	Revision string `json:"revision"`
	Verified bool   `json:"verified"`      // recipe.json records an admitted verification
	Here     bool   `json:"verified_here"` // this revision passed verification on this machine
	Source   Source `json:"source"`
	Problem  string `json:"problem,omitempty"` // why it cannot run yet, for a folder that does not validate
	// What a run takes and gives, so an agent can call it right first time.
	Inputs   []recipe.File    `json:"inputs,omitempty"`
	Outputs  []recipe.File    `json:"outputs,omitempty"`
	Settings []recipe.Setting `json:"settings,omitempty"`
}

// FromFolder describes the recipe folder at the root of fsys. A folder that
// does not validate still becomes an entry, carrying the reason, so a draft
// being written shows up as one.
func FromFolder(fsys fs.FS, dir string, source Source) Entry {
	e := Entry{Recipe: dir, Source: source}
	f, err := recipe.Load(fsys)
	if err != nil {
		e.Problem = err.Error()
		return e
	}
	rev, err := recipe.Revision(fsys)
	if err != nil {
		e.Problem = err.Error()
		return e
	}
	e.Recipe, e.What, e.Kind, e.Repo, e.Revision = f.ID.String(), f.Recipe.Task, f.Recipe.Kind, f.Recipe.Repo, rev
	e.Verified = f.Recipe.Verified != nil
	e.Inputs, e.Outputs, e.Settings = f.Recipe.Inputs, f.Recipe.Outputs, f.Recipe.Settings
	return e
}

// Index is the entries of every source, without duplicates.
type Index []Entry

// Merge gathers sources in order of precedence: when two hold the same
// recipe, the earlier source's entry is kept.
func Merge(sources ...[]Entry) Index {
	seen := map[string]bool{}
	var ix Index
	for _, entries := range sources {
		for _, e := range entries {
			if !seen[e.Recipe] {
				seen[e.Recipe] = true
				ix = append(ix, e)
			}
		}
	}
	sort.SliceStable(ix, func(i, j int) bool { return ix[i].Recipe < ix[j].Recipe })
	return ix
}

// Project returns the entries for owner/repo, whatever its case.
func (ix Index) Project(owner, repo string) []Entry {
	prefix := strings.ToLower(owner + "/" + repo + "/")
	var out []Entry
	for _, e := range ix {
		if strings.HasPrefix(e.Recipe, prefix) {
			out = append(out, e)
		}
	}
	return out
}

// Search returns the entries whose recipe ID or description holds every word.
func (ix Index) Search(query string) []Entry {
	words := strings.Fields(strings.ToLower(query))
	var out []Entry
	for _, e := range ix {
		text := strings.ToLower(e.Recipe + " " + e.What)
		match := len(words) > 0
		for _, w := range words {
			match = match && strings.Contains(text, w)
		}
		if match {
			out = append(out, e)
		}
	}
	return out
}

// NoRecipe is the answer for a project the catalogue has no recipe for yet.
type NoRecipe struct {
	Project string
	Task    string   // the task asked for, if any
	Tasks   []string // the project's other tasks, when it has some
}

func (e *NoRecipe) Error() string {
	if e.Task != "" && len(e.Tasks) > 0 {
		return fmt.Sprintf("%s has no %s recipe yet; it has %s", e.Project, e.Task, strings.Join(e.Tasks, ", "))
	}
	if e.Task != "" {
		return fmt.Sprintf("%s has no %s recipe yet", e.Project, e.Task)
	}
	return fmt.Sprintf("%s has no recipe yet", e.Project)
}

// ChooseTask is the answer for a project with several recipes and no task
// named.
type ChooseTask struct {
	Project string
	Choices []Entry
}

func (e *ChooseTask) Error() string {
	tasks := make([]string, len(e.Choices))
	for i, c := range e.Choices {
		tasks[i] = taskOf(c.Recipe) + " (" + c.What + ")"
	}
	return fmt.Sprintf("%s has %d recipes; name the task: %s", e.Project, len(e.Choices), strings.Join(tasks, ", "))
}

// Resolve finds the recipe for a project and, when it has several, a task.
func (ix Index) Resolve(owner, repo, task string) (Entry, error) {
	project := strings.ToLower(owner + "/" + repo)
	entries := ix.Project(owner, repo)
	if task != "" {
		for _, e := range entries {
			if taskOf(e.Recipe) == task {
				return e, nil
			}
		}
		tasks := make([]string, len(entries))
		for i, e := range entries {
			tasks[i] = taskOf(e.Recipe)
		}
		return Entry{}, &NoRecipe{Project: project, Task: task, Tasks: tasks}
	}
	switch len(entries) {
	case 0:
		return Entry{}, &NoRecipe{Project: project}
	case 1:
		return entries[0], nil
	}
	return Entry{}, &ChooseTask{Project: project, Choices: entries}
}

func taskOf(id string) string { return id[strings.LastIndex(id, "/")+1:] }
