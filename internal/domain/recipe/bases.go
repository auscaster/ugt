package recipe

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Base is an official image a recipe starts from.
type Base struct {
	Ref string // the image by tag; a recipe names it by digest
	For string // the projects it suits
}

// The bases drafted recipes start from. Recipes that share a base share its
// layers on the user's machine, so one download serves them all. A recipe may
// start from another base its project needs; every base is named by digest, so
// a build that passed verification builds the same way for its users.
var (
	BasePython = Base{"python:3.12-slim-bookworm", "Python projects"}
	BaseNode   = Base{"node:22-bookworm-slim", "JavaScript and TypeScript projects"}
	BaseGo     = Base{"golang:1.25-bookworm", "building Go projects, finished on Debian"}
	BaseRust   = Base{"rust:1-bookworm", "building Rust projects, finished on Debian"}
	BaseDebian = Base{"debian:bookworm-slim", "anything else, and a build's final stage"}
	Bases      = []Base{BasePython, BaseNode, BaseGo, BaseRust, BaseDebian}
)

// lintBases finds every Dockerfile a build in ./recipe uses and reports each
// base it names without a digest. A build from a Git context runs upstream's
// own Dockerfile at its pinned commit, so only the recipe's own are read.
func lintBases(fsys fs.FS, compose []byte) []string {
	var doc struct {
		Services map[string]struct {
			Build any `yaml:"build"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(compose, &doc); err != nil {
		return nil // lintCompose reports it
	}
	names := make([]string, 0, len(doc.Services))
	for name := range doc.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	var problems []string
	for _, name := range names {
		context, dockerfile, inline := ".", "Dockerfile", ""
		switch b := doc.Services[name].Build.(type) {
		case nil:
			continue
		case string:
			context = b
		case map[string]any:
			if c, ok := b["context"].(string); ok {
				context = c
			}
			if d, ok := b["dockerfile"].(string); ok {
				dockerfile = d
			}
			inline, _ = b["dockerfile_inline"].(string)
		}
		if !inRecipe(context) {
			continue // a Git context, or one lintCompose refuses
		}
		at := fmt.Sprintf("services.%s's Dockerfile", name)
		text := inline
		if text == "" {
			file := path.Join(strings.TrimPrefix(strings.TrimPrefix(path.Clean(context), "recipe"), "/"), dockerfile)
			data, err := fs.ReadFile(fsys, file)
			if err != nil {
				problems = append(problems, fmt.Sprintf("services.%s builds from recipe/%s, which the recipe does not have", name, file))
				continue
			}
			text, at = string(data), "recipe/"+file
		}
		for _, base := range unpinnedBases(text) {
			problems = append(problems, fmt.Sprintf(
				"%s starts FROM %s without a digest: name it by digest (%s@sha256:...), so the build its users run is the one verified", at, base, base))
		}
	}
	return dedupe(problems)
}

// unpinnedBases lists the bases a Dockerfile names without a digest. A stage
// it built earlier, and scratch, need none.
func unpinnedBases(dockerfile string) []string {
	stages := map[string]bool{"scratch": true}
	var unpinned []string
	for _, line := range strings.Split(dockerfile, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "FROM") {
			continue
		}
		args := fields[1:]
		for len(args) > 0 && strings.HasPrefix(args[0], "--") {
			args = args[1:]
		}
		if len(args) == 0 {
			continue
		}
		if image := args[0]; !stages[strings.ToLower(image)] && !strings.Contains(image, "@sha256:") {
			unpinned = append(unpinned, image)
		}
		if len(args) >= 3 && strings.EqualFold(args[1], "AS") {
			stages[strings.ToLower(args[2])] = true
		}
	}
	return unpinned
}
