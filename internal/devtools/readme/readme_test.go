package readme

import (
	"flag"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the generated blocks")

const root = "../../.."

// The skill's pitfalls table must match the diagnosis rules; -update
// regenerates it.
func TestSkillPitfallsAreCurrent(t *testing.T) {
	const file = root + "/skills/ugt/references/pitfalls.md"
	page, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Replace(string(page), "diagnosed", Pitfalls())
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile(file, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if want != string(page) {
		t.Fatal("pitfalls.md's table is out of date with the diagnosis rules; run: go test ./internal/devtools/readme -update")
	}
}

func TestReadmeRecipeLinksResolve(t *testing.T) {
	readme, err := os.ReadFile(root + "/README.md")
	if err != nil {
		t.Fatal(err)
	}
	if dead := DeadRecipeLinks(string(readme), root); len(dead) > 0 {
		t.Fatalf("README links to recipes that do not exist: %v", dead)
	}
}

func TestDeadRecipeLinks(t *testing.T) {
	got := DeadRecipeLinks("[a](recipes/openai/whisper/subtitles) [b](recipes/nobody/nothing/x) [c](recipes) [d](recipes/README.md)", root)
	if strings.Join(got, ",") != "recipes/nobody/nothing/x" {
		t.Fatalf("got %v", got)
	}
}

func TestReplaceNeedsMarkers(t *testing.T) {
	if _, err := Replace("# no markers\n", "catalogue", "x"); err == nil {
		t.Fatal("expected an error without markers")
	}
}
