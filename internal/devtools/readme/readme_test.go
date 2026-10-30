package readme

import (
	"flag"
	"os"
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

func TestReplaceNeedsMarkers(t *testing.T) {
	if _, err := Replace("# no markers\n", "catalogue", "x"); err == nil {
		t.Fatal("expected an error without markers")
	}
}
