package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/domain/report"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// The fictional job recipe from the spec: its compose.yaml builds task.
const exampleJob = "../../spec/recipe/v0/examples/valid/example-job"

func jobConfig() ComposeConfig {
	return ComposeConfig{Services: map[string]ComposeService{
		"task": {Builds: true}, "check": {}, "sample": {}, "sample-check": {},
	}}
}

// services returns a working recipe: the task writes the converted document,
// and the checks read it. strongCheck controls whether check really checks.
func services(strongCheck bool) map[string]func(string, io.Writer) int {
	out := func(dir string) string { return filepath.Join(dir, "outputs", "document.md") }
	read := func(dir, want string) int {
		data, err := os.ReadFile(out(dir))
		if err != nil || !strings.HasPrefix(string(data), want) {
			return 1
		}
		return 0
	}
	return map[string]func(string, io.Writer) int{
		"sample": func(string, io.Writer) int { return 0 },
		"task": func(dir string, _ io.Writer) int {
			if os.WriteFile(out(dir), []byte("# Expected heading\n\nBody text.\n"), 0o644) != nil {
				return 1
			}
			return 0
		},
		"check": func(dir string, _ io.Writer) int {
			if !strongCheck {
				return 0
			}
			return read(dir, "#")
		},
		"sample-check": func(dir string, _ io.Writer) int { return read(dir, "# Expected heading") },
	}
}

func newApp(t *testing.T, engine *fakeEngine) (*App, *memLedger) {
	t.Helper()
	root := t.TempDir()
	led := &memLedger{}
	return &App{
		Runtime: &fakeRuntime{root: root},
		Engine:  engine,
		Disk:    osDisk{},
		Ledger:  led,
		Layout:  workspace.Layout{Home: filepath.Join(root, "home"), State: filepath.Join(root, "state"), Workspaces: root},
		Now:     fixedClock(),
	}, led
}

func TestVerifyPassesAStrongRecipe(t *testing.T) {
	engine := &fakeEngine{config: jobConfig(), services: services(true)}
	a, led := newApp(t, engine)
	rep, err := a.Verify(context.Background(), exampleJob, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Passed {
		t.Fatalf("expected a pass, got %+v", rep)
	}
	got := strings.Join(engine.ran[:5], ",")
	if got != "build,sample,task,check,sample-check" {
		t.Errorf("steps ran in order %s", got)
	}
	if len(rep.Strength) != 6 || len(rep.WeakChecks()) != 0 {
		t.Errorf("expected six rejected probes, got %+v", rep.Strength)
	}
	if rep.Seconds <= 0 {
		t.Errorf("report duration %v, want positive", rep.Seconds)
	}
	if rep.Recipe != "example/tool/markdown" || len(rep.Revision) != 12 || rep.Arch != "arm64" {
		t.Errorf("report identity %+v", rep)
	}
	if engine.downs != 1 {
		t.Errorf("compose down ran %d times, want 1", engine.downs)
	}
	// The verified outputs are restored after the probes.
	data, err := os.ReadFile(filepath.Join(rep.Workspace, "outputs", "document.md"))
	if err != nil || !strings.HasPrefix(string(data), "# Expected heading") {
		t.Errorf("outputs not restored: %q %v", data, err)
	}
	if len(led.entries) == 0 {
		t.Error("nothing recorded in the ledger")
	}
}

func TestVerifyFailsAToothlessCheck(t *testing.T) {
	engine := &fakeEngine{config: jobConfig(), services: services(false)}
	a, _ := newApp(t, engine)
	rep, err := a.Verify(context.Background(), exampleJob, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed {
		t.Fatal("a check that accepts anything passed the check-strength test")
	}
	weak := rep.WeakChecks()
	var mutations []string
	for _, p := range weak {
		if p.Check != "check" {
			t.Errorf("sample-check was reported weak: %+v", p)
		}
		mutations = append(mutations, p.Mutation)
	}
	if strings.Join(mutations, ",") != "missing,empty,garbage" {
		t.Errorf("weak against %v", mutations)
	}
}

func TestVerifyDiagnosesAFailedBuild(t *testing.T) {
	engine := &fakeEngine{config: jobConfig(), services: services(true), build: func(w io.Writer) int {
		fmt.Fprintln(w, "ImportError: libGL.so.1: cannot open shared object file")
		return 1
	}}
	a, _ := newApp(t, engine)
	rep, err := a.Verify(context.Background(), exampleJob, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	step, failed := rep.FailedStep()
	if rep.Passed || !failed || step.Name != report.StepBuild {
		t.Fatalf("expected the build to fail, got %+v", rep)
	}
	if len(step.Findings) == 0 || step.Findings[0].Rule != "opengl" {
		t.Errorf("findings %+v", step.Findings)
	}
	if len(engine.ran) != 1 {
		t.Errorf("steps after a failed build ran: %v", engine.ran)
	}
	if engine.downs != 1 {
		t.Errorf("compose down ran %d times after a failure, want 1", engine.downs)
	}
}

func TestVerifyRejectsAnInvalidRecipe(t *testing.T) {
	a, _ := newApp(t, &fakeEngine{config: jobConfig(), services: services(true)})
	if _, err := a.Verify(context.Background(), "../../spec/recipe/v0/examples/invalid/privileged", VerifyOptions{}); err == nil {
		t.Fatal("an invalid recipe was verified")
	}
}
