//go:build !darwin && !windows

package linkhandler

import (
	"os"
	"strings"
	"testing"
)

// The desktop entry takes ugt:// links and passes them to ugt, its words
// quoted as desktop entries quote them.
func TestEntryTakesUgtLinks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home) // xdg-mime, where there is one, writes the default here
	h := New(home)
	changed, err := h.Register([]string{`/home/me/my "tools"/ugt`, "open"})
	if err != nil || !changed {
		t.Fatalf("%v, %v", changed, err)
	}
	data, _ := os.ReadFile(h.path())
	for _, want := range []string{`Exec="/home/me/my \\"tools\\"/ugt" "open" %u`, "MimeType=x-scheme-handler/ugt;"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("no %s in\n%s", want, data)
		}
	}
	if again, _ := h.Register([]string{`/home/me/my "tools"/ugt`, "open"}); again {
		t.Error("written again for the same command")
	}
	if err := h.Unregister(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(h.path()); !os.IsNotExist(err) {
		t.Error("the entry stayed")
	}
}
