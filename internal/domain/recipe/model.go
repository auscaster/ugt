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
	Format      = "ugt-draft-3"
)

// Kinds.
const (
	KindJob     = "job"     // the task service runs to completion and writes outputs/
	KindService = "service" // the stack keeps running for the user
)

// Conventional services and the profiles that hide the optional ones.
const (
	ServiceTask        = "task"
	ServiceCheck       = "check"
	ServiceSample      = "sample"
	ServiceSampleCheck = "sample-check"
	ProfileCheck       = "check"
	ProfileSample      = "sample"
)

// Executor variables a compose.yaml may read.
const (
	VarImage = "UGT_IMAGE"
	VarArch  = "UGT_ARCH"
)

// Recipe is recipe.json.
type Recipe struct {
	Format          string    `json:"recipe_format"`
	ID              string    `json:"id"`
	Repo            string    `json:"repo"`
	Commit          string    `json:"commit"`
	Release         *Release  `json:"release,omitempty"`
	Task            string    `json:"task"`
	Kind            string    `json:"kind"`
	Packaging       string    `json:"packaging"`
	PackagingNote   string    `json:"packaging_note,omitempty"`
	AdaptedFrom     string    `json:"adapted_from,omitempty"`
	PlatformsTested []string  `json:"platforms_tested,omitempty"`
	Settings        []Setting `json:"settings,omitempty"`
	Inputs          []File    `json:"inputs,omitempty"`
	Outputs         []File    `json:"outputs,omitempty"`
	KnownIssues     []string  `json:"known_issues,omitempty"`
	Licence         string    `json:"licence"`
	Verified        *Verified `json:"verified,omitempty"`
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

// File is a workspace path under inputs/ or outputs/.
type File struct {
	Path string `json:"path"`
	What string `json:"what"`
}

// Verified records the verification a recipe last passed.
type Verified struct {
	Date     string `json:"date"`
	Agent    string `json:"agent"`
	Evidence string `json:"evidence"`
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
