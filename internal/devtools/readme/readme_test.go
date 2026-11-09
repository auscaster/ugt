package readme

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"slices"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/usegeekthing/ugt/spec"
)

var update = flag.Bool("update", false, "rewrite the generated blocks")

const root = "../../.."

// The skill's generated tables must match their sources: the pitfalls the
// diagnosis rules, the bases the list drafts start from. -update regenerates
// them.
func TestSkillTablesAreCurrent(t *testing.T) {
	for _, c := range []struct{ file, marker, block string }{
		{root + "/skills/ugt/references/pitfalls.md", "diagnosed", Pitfalls()},
		{root + "/skills/ugt/references/recipe.md", "bases", Bases()},
	} {
		page, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatal(err)
		}
		want, err := Replace(string(page), c.marker, c.block)
		if err != nil {
			t.Fatal(err)
		}
		if *update {
			if err := os.WriteFile(c.file, []byte(want), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if want != string(page) {
			t.Errorf("%s's %s table is out of date; run: go test ./internal/devtools/readme -update", c.file, c.marker)
		}
	}
}

func TestReplaceNeedsMarkers(t *testing.T) {
	if _, err := Replace("# no markers\n", "catalogue", "x"); err == nil {
		t.Fatal("expected an error without markers")
	}
}

// Every recipe the README features is prebuilt, so the showcase never waits on
// a build; the list itself is in spec/catalogue/v0's prebuilt format.
func TestFeaturedRecipesArePrebuilt(t *testing.T) {
	page, err := os.ReadFile(root + "/README.md")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(root + "/recipes/prebuilt.json")
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	for url, doc := range map[string][]byte{spec.RecipeV0URL: spec.RecipeV0, spec.CatalogueV0URL: spec.CatalogueV0} {
		parsed, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddResource(url, parsed); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := c.Compile(spec.CatalogueV0URL + "#/$defs/prebuilt")
	if err != nil {
		t.Fatal(err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(inst); err != nil {
		t.Fatalf("recipes/prebuilt.json is outside spec/catalogue/v0: %v", err)
	}
	var list struct{ Recipes []string }
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	featured := Featured(string(page))
	if len(featured) == 0 {
		t.Fatal("the README features no recipe")
	}
	for _, id := range featured {
		if !slices.Contains(list.Recipes, id) {
			t.Errorf("the README features %s, which recipes/prebuilt.json does not list", id)
		}
	}
}
