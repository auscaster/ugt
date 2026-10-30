package readme

import (
	"flag"
	"os"
	"sort"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the README's generated blocks")

const root = "../../.."

// The README's generated blocks must match recipes/; go test ./internal/readme
// -update regenerates them.
func TestReadmeIsCurrent(t *testing.T) {
	c, err := Count(root)
	if err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(root + "/README.md")
	if err != nil {
		t.Fatal(err)
	}
	want := string(readme)
	blocks := c.Blocks()
	names := make([]string, 0, len(blocks))
	for name := range blocks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if want, err = Replace(want, name, blocks[name]); err != nil {
			t.Fatal(err)
		}
	}
	if *update {
		if err := os.WriteFile(root+"/README.md", []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if want != string(readme) {
		t.Fatal("README.md's generated blocks are out of date; run: go test ./internal/readme -update")
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

func TestBlocks(t *testing.T) {
	all := Catalogue{Recipes: 4, Verified: 4}.Blocks()
	if !strings.Contains(all["catalogue"], "every one verified") || !strings.Contains(all["recipes-badge"], "recipes-4%20verified") {
		t.Fatalf("all verified: %v", all)
	}
	some := Catalogue{Recipes: 5, Verified: 3}.Blocks()
	if !strings.Contains(some["catalogue"], "**5 recipes** so far, 3 verified") {
		t.Fatalf("some verified: %v", some)
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
