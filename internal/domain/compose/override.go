// Package compose is UGT's model of Compose: the resolved model of what will
// really run (Parse), the override the helper adds to every run, and (with
// Adopt) existing Compose files rewritten to the recipe rules.
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

// NoNewPrivileges is the security option every unprivileged service runs with.
const NoNewPrivileges = "no-new-privileges:true"

// Override returns the Compose override the helper adds to every run of a
// recipe, from its resolved model: every service carries the recipe's labels,
// and no-new-privileges unless it runs privileged (which the recipe declares);
// every image it builds is labelled with the recipe and revision; and a CPU
// limit above the machine's cpus (when known) is lowered to it, because Docker
// refuses such a limit rather than lowering it.
func Override(m Model, recipeID, revision string, cpus int) ([]byte, error) {
	labels := map[string]string{LabelRecipe: recipeID, LabelRevision: revision}
	names := make([]string, 0, len(m.Services))
	for name := range m.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	out := map[string]any{}
	for _, name := range names {
		s := m.Services[name]
		svc := map[string]any{"labels": labels}
		if !s.Privileged && !hasNoNewPrivileges(s.SecurityOpt) {
			svc["security_opt"] = []string{NoNewPrivileges}
		}
		if s.Builds() {
			svc["build"] = map[string]any{"labels": labels}
		}
		if cpus > 0 && s.CPUs > float64(cpus) {
			svc["cpus"] = float64(cpus)
		}
		out[name] = svc
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
