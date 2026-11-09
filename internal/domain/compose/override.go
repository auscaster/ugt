// Package compose is UGT's model of Compose: the resolved model of what will
// really run (Parse), the override the helper adds to every run, and (with
// Adopt) existing Compose files rewritten to the recipe rules.
package compose

import (
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Labels UGT puts on what it builds and runs: the recipe, and the run
// revision its images are built from.
const (
	LabelRecipe   = "com.usegeekthing.recipe"
	LabelRevision = "com.usegeekthing.rev"
)

// OverrideFile is where the helper writes its override, relative to the
// workspace.
const OverrideFile = ".ugt/override.yaml"

// NoNewPrivileges is the security option every unprivileged service runs with.
const NoNewPrivileges = "no-new-privileges:true"

// Publish is a port UGT publishes for what a recipe provides: a service's
// container port, on 127.0.0.1 at a host port UGT chose.
type Publish struct {
	Service string
	Target  int
	Host    int
}

// Override returns the Compose override the helper adds to every run of a
// recipe, from its resolved model: every service carries the recipe's labels,
// and no-new-privileges unless it runs privileged (which the recipe declares);
// every image it builds is labelled with the recipe and run revision; a CPU
// limit above the machine's cpus (when known) is lowered to it, because
// Docker refuses such a limit rather than lowering it; and each port in
// publish is published on 127.0.0.1.
func Override(m Model, recipeID, run string, cpus int, publish []Publish) ([]byte, error) {
	labels := map[string]string{LabelRecipe: recipeID, LabelRevision: run}
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
		var ports []map[string]any
		for _, p := range publish {
			if p.Service == name {
				ports = append(ports, map[string]any{"target": p.Target, "host_ip": "127.0.0.1", "published": strconv.Itoa(p.Host)})
			}
		}
		if ports != nil {
			svc["ports"] = ports
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
