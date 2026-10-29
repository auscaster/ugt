package skilllint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestShippedSkills lints every skill in the repository.
func TestShippedSkills(t *testing.T) {
	dirs, err := filepath.Glob("../../skills/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("no skills found")
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			problems, err := Lint(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range problems {
				t.Error(p)
			}
		})
	}
}

func TestFindsProblems(t *testing.T) {
	const goodFront = "---\nname: demo\ndescription: Does a thing.\n---\n"
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"clean", map[string]string{"SKILL.md": goodFront + "See [ref](references/a.md).\n", "references/a.md": "ok\n"}, ""},
		{"no frontmatter", map[string]string{"SKILL.md": "# Demo\n"}, "missing frontmatter"},
		{"name mismatch", map[string]string{"SKILL.md": "---\nname: other\ndescription: x\n---\n"}, "must match the directory name"},
		{"bad name", map[string]string{"SKILL.md": "---\nname: Demo_Skill\ndescription: x\n---\n"}, "lowercase words"},
		{"no description", map[string]string{"SKILL.md": "---\nname: demo\n---\n"}, "description is required"},
		{"unknown key", map[string]string{"SKILL.md": "---\nname: demo\ndescription: x\nversion: 1\n---\n"}, `unknown frontmatter key "version"`},
		{"broken link", map[string]string{"SKILL.md": goodFront + "[x](references/missing.md)\n"}, "does not resolve"},
		{"escaping link", map[string]string{"SKILL.md": goodFront + "[x](../outside.md)\n"}, "leaves the skill directory"},
		{"bad json", map[string]string{"SKILL.md": goodFront + "```json\n{ nope\n```\n"}, "does not parse"},
		{"bad recipe", map[string]string{"SKILL.md": goodFront + "```json\n{\"recipe_format\": \"ugt-draft-2\"}\n```\n"}, "not a valid recipe"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "demo")
			for name, body := range c.files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			problems, err := Lint(dir)
			if err != nil {
				t.Fatal(err)
			}
			var all []string
			for _, p := range problems {
				all = append(all, p.String())
			}
			joined := strings.Join(all, "\n")
			if c.want == "" && len(problems) > 0 {
				t.Fatalf("expected no problems, got:\n%s", joined)
			}
			if c.want != "" && !strings.Contains(joined, c.want) {
				t.Fatalf("expected a problem containing %q, got:\n%s", c.want, joined)
			}
		})
	}
}
