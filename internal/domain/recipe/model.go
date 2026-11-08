// Package recipe is the domain model of a UGT recipe: a folder whose
// compose.yaml holds everything that runs and whose recipe.json says what it
// is. It validates, loads and identifies recipes; it never runs them.
package recipe

import (
	"encoding/json"
	"fmt"
	"io/fs"
)

// File names and the current format.
const (
	RecordFile  = "recipe.json"
	ComposeFile = "compose.yaml"
	Format      = "ugt-draft-5"
)

// Kinds.
const (
	KindJob     = "job"     // the task service runs to completion and writes outputs/
	KindService = "service" // the stack keeps running for the user
)

// Conventional services and the profiles that hide the optional ones: the
// check and sample services, and the services of what a recipe provides to
// run on demand.
const (
	ServiceTask        = "task"
	ServiceCheck       = "check"
	ServiceSample      = "sample"
	ServiceSampleCheck = "sample-check"
	ProfileCheck       = "check"
	ProfileSample      = "sample"
	ProfileCommand     = "command"
	ProfileMCP         = "mcp"
)

// Executor variables a compose.yaml may read.
const (
	VarImage = "UGT_IMAGE"
	VarArch  = "UGT_ARCH"
)

// Recipe is recipe.json.
type Recipe struct {
	Format        string    `json:"recipe_format"`
	ID            string    `json:"id"`
	Repo          string    `json:"repo"`
	Commit        string    `json:"commit"`
	Release       *Release  `json:"release,omitempty"`
	Task          string    `json:"task"`
	Kind          string    `json:"kind"`
	Provides      Provides  `json:"provides,omitzero"`
	Packaging     string    `json:"packaging"`
	PackagingNote string    `json:"packaging_note,omitempty"`
	AdaptedFrom   string    `json:"adapted_from,omitempty"`
	Settings      []Setting `json:"settings,omitempty"`
	Access        []Access  `json:"access,omitempty"`
	Inputs        []File    `json:"inputs,omitempty"`
	Outputs       []File    `json:"outputs,omitempty"`
	KnownIssues   []string  `json:"known_issues,omitempty"`
	Licence       string    `json:"licence"`
	// Job is the catalogue's job it does, from spec/catalogue/v0/jobs.json.
	// ugt never acts on it; the catalogue groups and names recipes by it.
	Job string `json:"x-job,omitempty"`
}

// Release is a release artifact used instead of a source build.
type Release struct {
	Tag      string `json:"tag"`
	Artifact string `json:"artifact"`
	SHA256   string `json:"sha256"`
}

// Setting is something the user supplies; it becomes ${Name} in compose.yaml.
type Setting struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"` // "secret" or "value"
	Required bool   `json:"required"`
	What     string `json:"what"`
	How      string `json:"how,omitempty"`
}

// Access is one kind of access beyond the sandbox the recipe declares it
// needs; the grants themselves are read from what Compose will run.
type Access struct {
	Kind string `json:"kind"`
	Why  string `json:"why"`
}

// File is a workspace path under inputs/ or outputs/.
type File struct {
	Path string `json:"path"`
	What string `json:"what"`
}

// Provides is what stays usable after a run.
type Provides struct {
	Pages      []Address   `json:"pages,omitempty"`     // what a person opens in the browser
	Endpoints  []Address   `json:"endpoints,omitempty"` // what other software calls
	Commands   []Command   `json:"commands,omitempty"`
	MCPServers []MCPServer `json:"mcp_servers,omitempty"`
}

// Address is a page or an endpoint: a port a service listens on, which UGT
// publishes on 127.0.0.1.
type Address struct {
	Service  string `json:"service"`
	Port     int    `json:"port"`
	Path     string `json:"path,omitempty"`
	Protocol string `json:"protocol,omitempty"` // an endpoint's URL scheme; http when empty
	What     string `json:"what"`
}

// Command is a command for the user's terminal: its service, run with their
// arguments in the folder they run it in.
type Command struct {
	Name    string `json:"name"`
	Service string `json:"service"`
	What    string `json:"what"`
}

// MCPServer is a service speaking MCP over stdin and stdout, for AI apps.
type MCPServer struct {
	Service string `json:"service"`
	What    string `json:"what"`
}

// Addresses are the pages, then the endpoints.
func (p Provides) Addresses() []Address {
	return append(append([]Address{}, p.Pages...), p.Endpoints...)
}

// Whats are what each thing provided says it is, for search and for the
// rules a recipe keeps.
func (p Provides) Whats() []string {
	var out []string
	for _, a := range p.Addresses() {
		out = append(out, a.What)
	}
	for _, c := range p.Commands {
		out = append(out, c.What)
	}
	for _, m := range p.MCPServers {
		out = append(out, m.What)
	}
	return out
}

// Folder is a valid recipe, parsed.
type Folder struct {
	Recipe  Recipe
	ID      ID
	Compose []byte
}

// Load validates the recipe at the root of fsys and returns it parsed.
func Load(fsys fs.FS) (*Folder, error) {
	if err := Validate(fsys); err != nil {
		return nil, err
	}
	data, err := fs.ReadFile(fsys, RecordFile)
	if err != nil {
		return nil, err
	}
	f := &Folder{}
	if err := json.Unmarshal(data, &f.Recipe); err != nil {
		return nil, fmt.Errorf("%s: %w", RecordFile, err)
	}
	if f.ID, err = ParseID(f.Recipe.ID); err != nil {
		return nil, err
	}
	if f.Compose, err = fs.ReadFile(fsys, ComposeFile); err != nil {
		return nil, err
	}
	return f, nil
}

// SettingNames returns the names of the recipe's settings.
func (r Recipe) SettingNames() []string {
	names := make([]string, len(r.Settings))
	for i, s := range r.Settings {
		names[i] = s.Name
	}
	return names
}
