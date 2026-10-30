package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/domain/compose"
	"github.com/auscaster/ugt/internal/domain/report"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// The fictional job recipe from the spec: its compose.yaml builds task.
const exampleJob = "../../spec/recipe/v0/examples/valid/example-job"

// jobConfig is the resolved model of the example job: offline, like the real
// one the engine contract test resolves.
func jobConfig() compose.Model {
	return compose.Model{Services: map[string]compose.Service{
		"task": {Build: &compose.Build{}, NetworkMode: "none"}, "check": {NetworkMode: "none"},
		"sample": {NetworkMode: "none"}, "sample-check": {NetworkMode: "none"},
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
		Runtime: &fakeRuntime{root: filepath.ToSlash(root)},
		Engine:  engine,
		Disk:    osDisk{},
		Ledger:  led,
		Layout:  workspace.Layout{Home: filepath.Join(root, "home"), State: filepath.Join(root, "state"), Workspaces: filepath.ToSlash(root)},
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

// A failed check can be fixed and re-run alone, in the same workspace and
// with the same image, without building again.
// A full pass is kept, so UGT knows this revision passed here.
func TestVerifyKeepsAPassingReport(t *testing.T) {
	a, _ := newApp(t, &fakeEngine{config: jobConfig(), services: services(true)})
	rep, err := a.Verify(context.Background(), exampleJob, VerifyOptions{})
	if err != nil || !rep.Passed {
		t.Fatalf("verify %+v, %v", rep, err)
	}
	if !a.passedHere(rep.Recipe, rep.Revision) || a.passedHere(rep.Recipe, "000000000000") {
		t.Error("the passing report was not kept by revision")
	}
}

func TestVerifyResumesFromAStep(t *testing.T) {
	broken := services(true)
	broken["sample-check"] = func(string, io.Writer) int { return 1 }
	engine := &fakeEngine{config: jobConfig(), services: broken}
	a, _ := newApp(t, engine)
	first, err := a.Verify(context.Background(), exampleJob, VerifyOptions{})
	if err != nil || first.Passed {
		t.Fatalf("first run: %+v, %v", first, err)
	}

	engine.services = services(true)
	engine.ran = nil
	again, err := a.Verify(context.Background(), exampleJob, VerifyOptions{From: report.StepSampleCheck})
	if err != nil {
		t.Fatal(err)
	}
	if !again.Passed || again.From != report.StepSampleCheck || again.Workspace != first.Workspace {
		t.Fatalf("resumed: %+v", again)
	}
	if got := strings.Join(engine.ran[:1], " "); got != "sample-check" {
		t.Errorf("resumed run started with %q, not sample-check: %v", got, engine.ran)
	}
	if again.Image != "ugt/example/tool/markdown:"+first.Revision {
		t.Errorf("resumed with image %q", again.Image)
	}
}

func TestVerifyFromNeedsAVerification(t *testing.T) {
	a, _ := newApp(t, &fakeEngine{config: jobConfig(), services: services(true)})
	_, err := a.Verify(context.Background(), exampleJob, VerifyOptions{From: report.StepCheck})
	if err == nil || !strings.Contains(err.Error(), "not been verified") {
		t.Fatalf("got %v", err)
	}
	if _, err := a.Verify(context.Background(), exampleJob, VerifyOptions{From: "build"}); err == nil {
		t.Fatal("--from build was accepted")
	}
}

func TestExec(t *testing.T) {
	engine := &fakeEngine{config: jobConfig(), services: services(true)}
	a, _ := newApp(t, engine)
	rep, err := a.Verify(context.Background(), exampleJob, VerifyOptions{})
	if err != nil || !rep.Passed {
		t.Fatalf("verify: %+v, %v", rep, err)
	}
	res, err := a.Exec(context.Background(), exampleJob, "check", []string{"ls", "outputs"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if res.Workspace != rep.Workspace || engine.commands[0].Dir != rep.Workspace || engine.ran[len(engine.ran)-1] != "check: ls outputs" {
		t.Fatalf("exec %+v ran %v", res, engine.ran)
	}
	if _, err := a.Exec(context.Background(), exampleJob, "nope", nil, io.Discard); err == nil || !strings.Contains(err.Error(), "has no service") {
		t.Errorf("unknown service: %v", err)
	}
}

// When a service recipe's stack does not start, the report carries what its
// containers logged, and the diagnosis reads it.
func TestVerifyServiceStartFailureCarriesLogs(t *testing.T) {
	cfg := compose.Model{Services: map[string]compose.Service{"app": {}, "check": {}, "sample": {}, "sample-check": {}}}
	svc := services(true)
	engine := &fakeEngine{config: cfg, services: svc, up: func(w io.Writer) int {
		fmt.Fprintln(w, "container app exited (137)")
		return 1
	}, logs: "app  | Killed\n"}
	a, _ := newApp(t, engine)
	rep, err := a.Verify(context.Background(), "../../spec/recipe/v0/examples/valid/example-service", VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	step, failed := rep.FailedStep()
	if !failed || step.Name != report.StepTask || !strings.Contains(step.Log, "app  | Killed") {
		t.Fatalf("step %+v", step)
	}
	if len(step.Findings) == 0 || step.Findings[0].Rule != "memory" {
		t.Errorf("findings %+v", step.Findings)
	}
}
