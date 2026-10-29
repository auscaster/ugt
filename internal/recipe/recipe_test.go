package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const examples = "../../spec/recipe/v0/examples"

func TestValidExamples(t *testing.T) {
	for _, path := range glob(t, "valid") {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if err := Validate(read(t, path)); err != nil {
				t.Fatalf("expected valid, got:\n%v", err)
			}
		})
	}
}

func TestInvalidExamples(t *testing.T) {
	for _, path := range glob(t, "invalid") {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if err := Validate(read(t, path)); err == nil {
				t.Fatal("expected a schema violation, got none")
			}
		})
	}
}

func TestRejectsNonJSON(t *testing.T) {
	if err := Validate([]byte("recipe_format: ugt-draft-1")); err == nil {
		t.Fatal("expected an error for non-JSON input")
	}
}

// A command rule fails as "'not' failed"; the error must say which rule.
func TestNamesFailedRule(t *testing.T) {
	err := Validate(read(t, filepath.Join(examples, "invalid", "fixed-container-name.json")))
	if err == nil || !strings.Contains(err.Error(), "at '/run': Container names start with $UGT_NAME.") {
		t.Fatalf("expected the rule's description for /run, got:\n%v", err)
	}
}

func glob(t *testing.T, kind string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(examples, kind, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatalf("no %s examples found", kind)
	}
	return paths
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
