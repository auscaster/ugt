// Package compose holds the Compose files UGT writes itself: the override the
// helper adds to every run, and (with Adopt) existing Compose files rewritten
// to the recipe rules.
package compose

import (
	"sort"

	"gopkg.in/yaml.v3"
)

// Labels UGT puts on what it builds and runs.
const (
	LabelRecipe   = "com.usegithubthing.recipe"
	LabelRevision = "com.usegithubthing.rev"
)

// OverrideFile is where the helper writes its override, relative to the
// workspace.
const OverrideFile = ".ugt/override.yaml"

// NoNewPrivileges is the security option every service runs with.
const NoNewPrivileges = "no-new-privileges:true"

// Service is what the override needs to know about one service.
type Service struct {
	Name        string
	Builds      bool
	SecurityOpt []string // what the recipe already sets, so nothing is added twice
	// CPUs, when set, caps the service's CPU limit: a recipe may allow more
	// CPUs than this machine has, which Docker refuses rather than lowers.
	CPUs float64
}

// Override returns the Compose override the helper adds to every run of a
// recipe: every service carries the recipe's labels and no-new-privileges,
// every image it builds is labelled with the recipe and revision, and a CPU
// limit above what the machine has is lowered to it.
func Override(services []Service, recipeID, revision string) ([]byte, error) {
	labels := map[string]string{LabelRecipe: recipeID, LabelRevision: revision}
	out := map[string]any{}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	for _, s := range services {
		svc := map[string]any{"labels": labels}
		if !hasNoNewPrivileges(s.SecurityOpt) {
			svc["security_opt"] = []string{NoNewPrivileges}
		}
		if s.Builds {
			svc["build"] = map[string]any{"labels": labels}
		}
		if s.CPUs > 0 {
			svc["cpus"] = s.CPUs
		}
		out[s.Name] = svc
	}
	return yaml.Marshal(map[string]any{"services": out})
}

// hasNoNewPrivileges reports whether a service already sets it, in either
// spelling Compose accepts.
func hasNoNewPrivileges(opts []string) bool {
	for _, o := range opts {
		if o == NoNewPrivileges || o == "no-new-privileges" {
			return true
		}
	}
	return false
}
