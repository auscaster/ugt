// Package skilllint checks UGT's agent skills before they ship.
//
// A skill is installed by copying its directory into an agent's configuration,
// so it must stand alone: valid frontmatter, links that stay inside the skill
// directory, and examples that parse and match the published schemas.
package skilllint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/auscaster/ugt/internal/domain/recipe"
)

// Problem is one lint finding, reported against a file in the skill.
type Problem struct {
	File    string
	Message string
}

func (p Problem) String() string { return p.File + ": " + p.Message }

var (
	namePattern  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	linkPattern  = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	fencePattern = regexp.MustCompile("(?s)```json\n(.*?)```")
	// Frontmatter keys defined by the Agent Skills format.
	allowedKeys = map[string]bool{
		"name": true, "description": true, "license": true,
		"allowed-tools": true, "metadata": true, "compatibility": true,
	}
)

// Lint checks the skill in dir and returns every problem found.
func Lint(dir string) ([]Problem, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	var problems []Problem
	add := func(file, format string, args ...any) {
		rel, _ := filepath.Rel(dir, file)
		problems = append(problems, Problem{File: rel, Message: fmt.Sprintf(format, args...)})
	}

	skillFile := filepath.Join(dir, "SKILL.md")
	data, err := os.ReadFile(skillFile)
	if err != nil {
		return nil, fmt.Errorf("read SKILL.md: %w", err)
	}
	lintFrontmatter(data, filepath.Base(dir), func(f string, a ...any) { add(skillFile, f, a...) })

	files, err := markdownFiles(dir)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		lintLinks(dir, file, body, func(f string, a ...any) { add(file, f, a...) })
		lintJSON(body, func(f string, a ...any) { add(file, f, a...) })
	}
	return problems, nil
}

func lintFrontmatter(data []byte, dirName string, add func(string, ...any)) {
	rest, ok := bytes.CutPrefix(data, []byte("---\n"))
	if !ok {
		add("missing frontmatter: file must start with ---")
		return
	}
	front, _, ok := bytes.Cut(rest, []byte("\n---\n"))
	if !ok {
		add("unterminated frontmatter")
		return
	}
	var fields map[string]any
	if err := yaml.Unmarshal(front, &fields); err != nil {
		add("frontmatter is not valid YAML: %v", err)
		return
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !allowedKeys[k] {
			add("unknown frontmatter key %q", k)
		}
	}
	name, _ := fields["name"].(string)
	switch {
	case name == "":
		add("frontmatter name is required")
	case len(name) > 64 || !namePattern.MatchString(name):
		add("name %q must be lowercase words joined by hyphens, at most 64 characters", name)
	case name != dirName:
		add("name %q must match the directory name %q", name, dirName)
	}
	desc, _ := fields["description"].(string)
	switch {
	case strings.TrimSpace(desc) == "":
		add("frontmatter description is required")
	case len(desc) > 1024:
		add("description is %d characters; the limit is 1024", len(desc))
	}
}

func lintLinks(dir, file string, body []byte, add func(string, ...any)) {
	for _, m := range linkPattern.FindAllSubmatch(body, -1) {
		target := string(m[1])
		if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "#") {
			continue
		}
		target, _, _ = strings.Cut(target, "#")
		resolved := filepath.Join(filepath.Dir(file), target)
		if rel, err := filepath.Rel(dir, resolved); err != nil || strings.HasPrefix(rel, "..") {
			add("link %q leaves the skill directory; installed skills must be self-contained", target)
			continue
		}
		if _, err := os.Stat(resolved); err != nil {
			add("link %q does not resolve", target)
		}
	}
}

func lintJSON(body []byte, add func(string, ...any)) {
	for i, m := range fencePattern.FindAllSubmatch(body, -1) {
		block := m[1]
		var value map[string]any
		if err := json.Unmarshal(block, &value); err != nil {
			add("json block %d does not parse: %v", i+1, err)
			continue
		}
		if _, isRecipe := value["recipe_format"]; isRecipe {
			if err := recipe.ValidateRecord(block); err != nil {
				add("json block %d is not a valid recipe:\n%v", i+1, err)
			}
		}
	}
}

func markdownFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
