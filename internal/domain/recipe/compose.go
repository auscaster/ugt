package recipe

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Services every recipe's compose.yaml names by convention. The executor runs
// them in order: sample (known input), task, check (any input), sample-check.
var conventional = []struct {
	name, profile, purpose string
}{
	{"task", "", "does the task on inputs/ and writes outputs/"},
	{"check", "check", "fails unless the task happened, for any input"},
	{"sample", "sample", "puts the known input into inputs/"},
	{"sample-check", "sample", "fails unless the sample's output has what it predicts"},
}

// Mountable are the workspace folders a service may bind-mount without
// declaring access, relative to the workspace root, which is the Compose
// project directory: the sandbox's own. inputs and outputs are mounted whole,
// since what runs can put a link inside them that a later mount of a folder
// within would follow; recipe, which nothing that runs can write, or any
// folder in it.
var Mountable = []string{"inputs", "outputs", "recipe"}

// Variables the executor sets; any other ${NAME} must be a declared setting,
// so nothing leaks in from the host's environment.
var executorVars = map[string]bool{"UGT_IMAGE": true, "UGT_ARCH": true}

// lintCompose checks a recipe's compose.yaml, with its record, against the
// rules a recipe must keep wherever it runs, and returns one message per
// broken rule.
func lintCompose(data []byte, r Recipe) []string {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return []string{fmt.Sprintf("compose.yaml is not valid YAML: %v", err)}
	}
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if _, ok := doc["name"]; ok {
		add("compose.yaml sets a project name; the executor names the project")
	}
	if doc["include"] != nil {
		add("compose.yaml includes other Compose files; a recipe keeps everything that runs in one file")
	}
	for _, key := range []string{"secrets", "configs"} {
		entries, _ := doc[key].(map[string]any)
		for _, name := range sortedKeys(entries) {
			entry, _ := entries[name].(map[string]any)
			if file, ok := entry["file"].(string); ok && !inRecipe(file) {
				add("%s.%s reads %q; files a recipe reads live in ./recipe", key, name, file)
			}
			if entry["environment"] != nil {
				add("%s.%s reads the host's environment; use a setting", key, name)
			}
		}
	}
	services, _ := doc["services"].(map[string]any)
	if len(services) == 0 {
		add("compose.yaml has no services")
		return problems
	}

	for _, c := range conventional {
		svc, ok := services[c.name].(map[string]any)
		switch {
		case c.name == ServiceTask && r.Kind == KindService:
			if ok {
				add("services.task must not be in a service recipe: up starts its stack, and its check proves it works")
			}
		case !ok:
			add("services.%s is missing: it %s", c.name, c.purpose)
		case c.profile == "" && svc["profiles"] != nil:
			add("services.%s must not have a profile; it runs by default", c.name)
		case c.profile != "" && !hasString(svc["profiles"], c.profile):
			add("services.%s needs profiles: [%s], so it runs only when the executor asks", c.name, c.profile)
		}
	}

	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		svc, _ := services[name].(map[string]any)
		lintService(name, svc, add)
	}

	lintProvides(r, services, add)

	declared := map[string]bool{}
	for _, s := range r.Settings {
		declared[s.Name] = true
	}
	used := map[string]bool{}
	for _, v := range ComposeVariables(doc) {
		used[v.Name] = true
		if !executorVars[v.Name] && !declared[v.Name] {
			add("${%s} is neither set by the executor nor a declared setting", v.Name)
		}
	}
	for _, s := range r.Settings {
		if !used[s.Name] {
			add("setting %s is declared but compose.yaml never uses it", s.Name)
		}
	}
	return dedupe(problems)
}

// lintProvides checks that what a recipe provides is there to provide, and
// fits its kind: only a job delivers outputs, and only a service keeps pages
// and endpoints running, on ports UGT publishes rather than compose.yaml; a
// command's or MCP server's service runs only when used.
func lintProvides(r Recipe, services map[string]any, add func(string, ...any)) {
	p := r.Provides
	switch r.Kind {
	case KindJob:
		if len(p.Pages)+len(p.Endpoints) > 0 {
			add("only a service recipe provides pages and endpoints: a job's stack stops when it is done")
		}
	case KindService:
		if len(r.Outputs) > 0 {
			add("outputs are what a job delivers; a service recipe provides pages and endpoints instead")
		}
	}
	for _, a := range p.Addresses() {
		svc, ok := services[a.Service].(map[string]any)
		switch {
		case !ok:
			add("recipe.json provides port %d of services.%s, which compose.yaml does not have", a.Port, a.Service)
		case svc["profiles"] != nil:
			add("recipe.json provides port %d of services.%s, which has a profile; it must run by default", a.Port, a.Service)
		case publishes(svc, a.Port):
			add("services.%s publishes port %d, which recipe.json provides: remove it from ports, because UGT publishes it on 127.0.0.1 at an address that stays the same", a.Service, a.Port)
		}
	}
	names := map[string]bool{}
	for _, c := range p.Commands {
		onDemand(services, "the command "+c.Name, c.Service, ProfileCommand, add)
		if names[c.Name] {
			add("recipe.json provides the command %s twice", c.Name)
		}
		names[c.Name] = true
	}
	for _, m := range p.MCPServers {
		onDemand(services, "the MCP server", m.Service, ProfileMCP, add)
	}
}

// onDemand checks the service of a command or MCP server: it exists, and its
// profile keeps it from starting with the stack.
func onDemand(services map[string]any, what, name, profile string, add func(string, ...any)) {
	svc, ok := services[name].(map[string]any)
	switch {
	case !ok:
		add("%s runs services.%s, which compose.yaml does not have", what, name)
	case !hasString(svc["profiles"], profile):
		add("services.%s needs profiles: [%s], so it runs only when %s is used", name, profile, what)
	}
}

// publishes reports whether a service publishes a container port, in either
// of Compose's forms.
func publishes(svc map[string]any, port int) bool {
	ports, _ := svc["ports"].([]any)
	for _, p := range ports {
		var target string
		switch p := p.(type) {
		case map[string]any:
			target = fmt.Sprint(p["target"])
		case string:
			spec, _, _ := strings.Cut(p, "/")
			target = spec[strings.LastIndex(spec, ":")+1:]
		case int:
			target = strconv.Itoa(p)
		}
		if target == strconv.Itoa(port) {
			return true
		}
	}
	return false
}

// ComposeVariables returns every variable a parsed Compose file reads. Compose
// interpolates values, never keys or comments.
func ComposeVariables(doc any) []Variable {
	var b strings.Builder
	var walk func(any)
	walk = func(n any) {
		switch n := n.(type) {
		case string:
			b.WriteString(n)
			b.WriteByte(0)
		case map[string]any:
			keys := make([]string, 0, len(n))
			for k := range n {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(n[k])
			}
		case []any:
			for _, item := range n {
				walk(item)
			}
		}
	}
	walk(doc)
	return Variables(b.String())
}

func lintService(name string, svc map[string]any, add func(string, ...any)) {
	at := "services." + name
	if _, ok := svc["container_name"]; ok {
		add("%s sets container_name; names come from the executor's project", at)
	}
	if svc["build"] != nil {
		image, _ := svc["image"].(string)
		if !strings.HasPrefix(image, "${UGT_IMAGE}") {
			add("%s builds an image, so its image must be ${UGT_IMAGE} or ${UGT_IMAGE}-<suffix>", at)
		}
		lintBuild(at, svc["build"], add)
	}
	if svc["extends"] != nil {
		add("%s extends another service; write what it needs into this one", at)
	}
	var envFiles []any
	switch e := svc["env_file"].(type) {
	case string:
		envFiles = []any{e}
	case []any:
		envFiles = e
	}
	for _, e := range envFiles {
		file, _ := e.(string)
		if m, ok := e.(map[string]any); ok {
			file, _ = m["path"].(string)
		}
		if !inRecipe(file) {
			add("%s reads env_file %q; files a recipe reads live in ./recipe", at, file)
		}
	}
}

// lintBuild keeps a build's inputs to the recipe folder or a remote source,
// so a build never reads the host's files.
func lintBuild(at string, build any, add func(string, ...any)) {
	context, dockerfile := ".", ""
	extra := map[string]any{}
	switch b := build.(type) {
	case string:
		context = b
	case map[string]any:
		if c, ok := b["context"].(string); ok {
			context = c
		}
		dockerfile, _ = b["dockerfile"].(string)
		// a map of name: source, or a list of name=source
		switch contexts := b["additional_contexts"].(type) {
		case map[string]any:
			extra = contexts
		case []any:
			for _, item := range contexts {
				s, _ := item.(string)
				name, source, _ := strings.Cut(s, "=")
				extra[name] = source
			}
		}
	}
	if !remoteContext(context) && !inRecipe(context) {
		add("%s builds from %q; build from ./recipe or a Git context pinned to a commit", at, context)
	}
	if dockerfile != "" && (path.IsAbs(dockerfile) || strings.HasPrefix(path.Clean(dockerfile), "..")) {
		add("%s reads the Dockerfile %q from outside its context", at, dockerfile)
	}
	for _, name := range sortedKeys(extra) {
		if c, _ := extra[name].(string); !remoteContext(c) && !strings.Contains(c, "://") && !inRecipe(c) {
			add("%s.additional_contexts.%s reads %q; use ./recipe or a remote source", at, name, c)
		}
	}
}

// remoteContext reports whether a build context is fetched rather than read
// from disk.
func remoteContext(c string) bool {
	return strings.HasPrefix(c, "https://") || strings.HasPrefix(c, "git@") || strings.HasPrefix(c, "docker-image://")
}

// inRecipe reports whether a workspace-relative path is the recipe folder or
// inside it.
func inRecipe(p string) bool {
	if !strings.HasPrefix(p, "./") && p != "." {
		return false
	}
	clean := path.Clean(p)
	return clean == "recipe" || strings.HasPrefix(clean, "recipe/")
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func hasString(list any, want string) bool {
	items, _ := list.([]any)
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func dedupe(items []string) []string {
	seen := map[string]bool{}
	out := items[:0]
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}
