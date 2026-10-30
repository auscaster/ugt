package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An agent writes recipe files only into the local catalogue's folders, and
// never outside the folder it names.
func TestWriteRecipeStaysInTheLocalCatalogue(t *testing.T) {
	a, _ := newApp(t, &fakeEngine{})
	dir := filepath.Join(a.Layout.Recipes(), "a", "b", "c")
	if _, err := a.WriteRecipe(dir, map[string]string{"check/x.sh": "true\n"}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "check", "x.sh")); err != nil || string(data) != "true\n" {
		t.Errorf("wrote %q, %v", data, err)
	}
	for name, c := range map[string]struct {
		dir  string
		file string
	}{
		"another folder":       {t.TempDir(), "authorized_keys"},
		"the catalogue itself": {a.Layout.Recipes(), "x"},
		"a path out":           {dir, "../../../../escape"},
		"an absolute path":     {dir, "/etc/passwd"},
	} {
		if _, err := a.WriteRecipe(c.dir, map[string]string{c.file: "x"}); err == nil {
			t.Errorf("%s: written", name)
		} else if name == "another folder" && !strings.Contains(err.Error(), "local catalogue") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
