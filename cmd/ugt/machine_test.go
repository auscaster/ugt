//go:build darwin || linux

package main

import (
	"testing"
	"time"

	"github.com/auscaster/ugt/internal/domain/ledger"
)

// An install with recipes in the engine it found keeps it when UGT's own
// machine comes to Linux; a new one, or one with the machine, runs the
// machine.
func TestAnInstallKeepsTheEngineItsRecipesAreIn(t *testing.T) {
	at := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	image := ledger.Entry{Kind: ledger.Image, Name: "ugt-a-b-c", Recipe: "a/b/c", Created: at}
	for _, c := range []struct {
		why     string
		entries []ledger.Entry
		keeps   bool
	}{
		{"a new install", nil, false},
		{"connections only", []ledger.Entry{{Kind: ledger.Connection, Name: "claude-code", Created: at}}, false},
		{"recipes in the engine it found", []ledger.Entry{image}, true},
		{"recipes in UGT's machine", []ledger.Entry{{Kind: ledger.Runtime, Name: "vm:UGT", Created: at}, image}, false},
		{"its recipes removed since", []ledger.Entry{image, {Kind: ledger.Image, Name: "ugt-a-b-c", Recipe: "a/b/c", Created: at, Removed: at}}, false},
	} {
		if got := usesItsEngine(c.entries); got != c.keeps {
			t.Errorf("%s: %v", c.why, got)
		}
	}
}
