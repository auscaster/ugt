// Package workspace names UGT's folders: the user-visible Home, the per-OS
// state directory, and each recipe's workspace in the runtime. It computes
// paths and never touches the disk.
package workspace

import (
	"path"
	"path/filepath"
	"time"

	"github.com/usegeekthing/ugt/internal/domain/recipe"
)

// Folders inside every workspace.
const (
	RecipeDir  = "recipe"
	InputsDir  = "inputs"
	OutputsDir = "outputs"
)

// Layout locates UGT's folders on one machine.
type Layout struct {
	// Home is the only folder users see: input files go in, results come out.
	// ~/UGT on macOS and Linux, %USERPROFILE%\UGT on Windows. A host path.
	Home string
	// State holds UGT's own records: config, ledger, jobs and logs. A host path.
	State string
	// Workspaces is where recipes run, in the runtime's file system: under
	// State on macOS and Linux, inside the UGT distro on Windows. Always a
	// slash-separated path.
	Workspaces string
}

// DefaultLayout returns the layout for goos, given the user's home folder and
// an environment lookup. Workspaces is filled in by the runtime.
func DefaultLayout(goos, userHome string, getenv func(string) string) Layout {
	l := Layout{Home: filepath.Join(userHome, "UGT")}
	switch goos {
	case "darwin":
		l.State = filepath.Join(userHome, "Library", "Application Support", "UGT")
	case "windows":
		base := getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(userHome, "AppData", "Local")
		}
		l.State = filepath.Join(base, "UGT")
	default:
		base := getenv("XDG_DATA_HOME")
		if base == "" {
			base = filepath.Join(userHome, ".local", "share")
		}
		l.State = filepath.Join(base, "ugt")
	}
	return l
}

// Results is where a recipe's results are delivered: Home/<owner>/<repo>/<task>.
func (l Layout) Results(id recipe.ID) string {
	return filepath.Join(l.Home, id.Owner, id.Repo, id.Task)
}

// Workspace is a recipe's workspace in the runtime.
func (l Layout) Workspace(id recipe.ID) string {
	return path.Join(l.Workspaces, id.Owner, id.Repo, id.Task)
}

// Scratch is a fresh workspace for one verification.
func (l Layout) Scratch(id recipe.ID, at time.Time) string {
	return path.Join(l.Workspaces, ".scratch", id.Owner+"."+id.Repo+"."+id.Task+"-"+at.UTC().Format("20060102T150405"))
}

// Probe is where a recipe is staged only to read what it would be granted,
// away from its workspace, which may be running.
func (l Layout) Probe(id recipe.ID) string {
	return path.Join(l.Workspaces, ".probe", id.Owner+"."+id.Repo+"."+id.Task)
}

// Cache is where UGT keeps what it fetched by content, such as the published
// catalogue's recipe folders, which never change once fetched.
func (l Layout) Cache() string { return filepath.Join(l.State, "cache") }

// Recipes is the local catalogue: recipes made on this machine, which find
// and run see beside the published catalogue.
func (l Layout) Recipes() string { return filepath.Join(l.State, "recipes") }

// LocalRecipe is a recipe's folder in the local catalogue.
func (l Layout) LocalRecipe(id recipe.ID) string {
	return filepath.Join(l.Recipes(), id.Owner, id.Repo, id.Task)
}

// Report is where a passing verification of a recipe revision is kept, so
// UGT knows what passed on this machine and submit can attach the evidence.
func (l Layout) Report(id recipe.ID, rev string) string {
	return filepath.Join(l.State, "reports", id.Owner, id.Repo, id.Task, rev+".json")
}

// Files UGT keeps in the state directory.
func (l Layout) Config() string   { return filepath.Join(l.State, "config.json") }
func (l Layout) Settings() string { return filepath.Join(l.State, "settings.json") }
func (l Layout) Consents() string { return filepath.Join(l.State, "consents.json") }
func (l Layout) Ledger() string   { return filepath.Join(l.State, "ledger.jsonl") }
func (l Layout) Jobs() string     { return filepath.Join(l.State, "jobs") }
func (l Layout) Logs() string     { return filepath.Join(l.State, "logs") }
