package app

import (
	"context"
	"testing"

	"github.com/usegeekthing/ugt/internal/domain/ledger"
)

func TestDoctor(t *testing.T) {
	engine := &fakeEngine{memory: 2 << 30}
	a, _ := newApp(t, engine)
	a.GitHub = &fakeGitHub{repos: map[string]Repo{"openai/whisper": whisper}}
	rep, err := a.Doctor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Check{}
	for _, c := range rep.Checks {
		byName[c.Name] = c
	}
	// little memory is a warning, not a failure: placement refuses what does not fit
	if !rep.OK || !byName["memory"].Warning || byName["memory"].Fix == "" || !byName["engine"].OK || !byName["registry"].OK || !byName["github"].OK {
		t.Fatalf("doctor %+v", rep)
	}
}

type creatingRuntime struct{ fakeRuntime }

func (creatingRuntime) Setup(context.Context, SetupOptions) (SetupResult, error) {
	return SetupResult{Runtime: "wsl", Ready: true, Created: "wsl:UGT"}, nil
}

func TestSetupRecordsWhatItCreates(t *testing.T) {
	a, led := newApp(t, &fakeEngine{})
	a.Runtime = &creatingRuntime{}
	if _, err := a.Setup(context.Background(), SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(led.entries) != 1 || led.entries[0].Kind != ledger.Runtime || led.entries[0].Name != "wsl:UGT" {
		t.Fatalf("ledger %+v", led.entries)
	}
}
