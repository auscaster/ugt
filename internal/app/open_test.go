package app

import (
	"context"
	"strings"
	"testing"

	"github.com/usegeekthing/ugt/internal/domain/ledger"
)

type fakeLinks struct {
	registered   [][]string
	unregistered int
}

func (f *fakeLinks) Register(command []string) (bool, error) {
	changed := len(f.registered) == 0
	f.registered = append(f.registered, command)
	return changed, nil
}
func (f *fakeLinks) Unregister() error { f.unregistered++; return nil }

// Setup makes ugt:// links open this ugt and records it once; uninstall
// undoes it.
func TestSetupMakesLinksOpenUGT(t *testing.T) {
	a, led := newApp(t, &fakeEngine{})
	links := &fakeLinks{}
	a.Links, a.Self = links, MCPServer{Command: "/opt/ugt", Args: []string{"mcp"}}
	res, err := a.Setup(context.Background(), SetupOptions{})
	if err != nil || strings.Join(links.registered[0], " ") != "/opt/ugt open" || !strings.Contains(strings.Join(res.Done, "; "), "Open in UGT links") {
		t.Fatalf("%+v, %v, %v", res, err, links.registered)
	}
	again, _ := a.Setup(context.Background(), SetupOptions{})
	if strings.Contains(strings.Join(again.Done, "; "), "Open in UGT links") {
		t.Errorf("said again: %v", again.Done)
	}
	var handlers int
	for _, e := range led.entries {
		if e.Kind == ledger.Handler {
			handlers++
		}
	}
	if handlers != 1 {
		t.Errorf("recorded %d times: %+v", handlers, led.entries)
	}
	if _, err := a.Uninstall(context.Background(), true); err != nil || links.unregistered != 1 {
		t.Errorf("uninstall: %v, %d", err, links.unregistered)
	}
}
