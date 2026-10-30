package recipe

import (
	"fmt"
	"path"
	"sort"
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

// Workspace folders a service may mount from the host, relative to the
// workspace root, which is the Compose project directory.
var mountable = []string{"inputs", "outputs", "recipe"}

// Variables the executor sets; any other ${NAME} must be a declared setting,
// so nothing leaks in from the host's environment.
var executorVars = map[string]bool{"UGT_IMAGE": true, "UGT_ARCH": true}

// lintCompose checks a recipe's compose.yaml against the rules a recipe must
// keep wherever it runs, and returns one message per broken rule.
func lintCompose(data []byte, kind string, settings []string) []string {
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
		case !ok && c.name == "task" && kind == "service":
			// a service recipe may simply keep running
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

	declared := map[string]bool{}
	for _, s := range settings {
		declared[s] = true
	}
	used := map[string]bool{}
	for _, v := range ComposeVariables(doc) {
		used[v.Name] = true
		if !executorVars[v.Name] && !declared[v.Name] {
			add("${%s} is neither set by the executor nor a declared setting", v.Name)
		}
	}
	for _, s := range settings {
		if !used[s] {
			add("setting %s is declared but compose.yaml never uses it", s)
		}
	}
	return dedupe(problems)
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
	if svc["privileged"] == true {
		add("%s is privileged", at)
	}
	for _, key := range []string{"network_mode", "pid", "ipc", "userns_mode"} {
		if svc[key] == "host" {
			add("%s uses the host's %s", at, strings.TrimSuffix(key, "_mode"))
		}
	}
	for _, key := range []string{"cap_add", "devices"} {
		if svc[key] != nil {
			add("%s sets %s; recipes get no extra host access", at, key)
		}
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
	if hosts := fmt.Sprint(svc["extra_hosts"]); strings.Contains(hosts, "host-gateway") {
		add("%s maps a name to host-gateway; recipes do not reach the host machine", at)
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
	volumes, _ := svc["volumes"].([]any)
	for _, v := range volumes {
		if source := volumeSource(v); source != "" {
			if problem := mountProblem(source); problem != "" {
				add("%s mounts %q: %s", at, source, problem)
			}
		}
	}
	ports, _ := svc["ports"].([]any)
	for _, p := range ports {
		if !localPort(p) {
			add("%s publishes %v beyond 127.0.0.1", at, p)
		}
	}
}

// lintBuild keeps a build's inputs to the recipe folder or a remote source,
// so a build never reads the host's files.
func lintBuild(at string, build any, add func(string, ...any)) {
	context, dockerfile := ".", ""
	var extra map[string]any
	switch b := build.(type) {
	case string:
		context = b
	case map[string]any:
		if c, ok := b["context"].(string); ok {
			context = c
		}
		dockerfile, _ = b["dockerfile"].(string)
		extra, _ = b["additional_contexts"].(map[string]any)
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

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// volumeSource returns the host side of a bind mount, or "" for a named volume.
func volumeSource(v any) string {
	switch v := v.(type) {
	case string:
		source, _, ok := strings.Cut(v, ":")
		if !ok || !(strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") || strings.HasPrefix(source, "~") || strings.HasPrefix(source, "$")) {
			return ""
		}
		return source
	case map[string]any:
		if v["type"] == "bind" {
			source, _ := v["source"].(string)
			return source
		}
	}
	return ""
}

func mountProblem(source string) string {
	if strings.Contains(source, "docker.sock") {
		return "the container engine's socket is never mounted"
	}
	if !strings.HasPrefix(source, "./") {
		return "only ./inputs, ./outputs and ./recipe in the workspace may be mounted"
	}
	clean := path.Clean(source)
	for _, dir := range mountable {
		if clean == dir || strings.HasPrefix(clean, dir+"/") {
			return ""
		}
	}
	return "only ./inputs, ./outputs and ./recipe in the workspace may be mounted"
}

func localPort(p any) bool {
	switch p := p.(type) {
	case string:
		return strings.HasPrefix(p, "127.0.0.1:")
	case map[string]any:
		return p["host_ip"] == "127.0.0.1"
	}
	return false
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
