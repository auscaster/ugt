package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/app"
)

const examples = "../../../spec/recipe/v0/examples/"

func run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	c := &CLI{App: &app.App{}, Stdout: &stdout, Stderr: &stderr, Version: "test"}
	status := c.Run(context.Background(), args)
	return status, stdout.String(), stderr.String()
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		status int
		out    string
	}{
		{"valid", []string{"recipe", "validate", examples + "valid/example-job"}, ExitOK, "ok"},
		{"recipe.json path", []string{"recipe", "validate", examples + "valid/example-job/recipe.json"}, ExitOK, "ok"},
		{"invalid", []string{"recipe", "validate", examples + "invalid/missing-check-service"}, ExitFailed, "services.check is missing"},
		{"missing", []string{"recipe", "validate", examples + "nope"}, ExitFailed, "invalid"},
		{"mixed", []string{"recipe", "validate", examples + "valid/example-job", examples + "invalid/short-commit"}, ExitFailed, "invalid"},
		{"no folders", []string{"recipe", "validate"}, ExitUsage, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, out, _ := run(c.args...)
			if status != c.status || !strings.Contains(out, c.out) {
				t.Fatalf("status %d, output %q", status, out)
			}
		})
	}
}

func TestUsage(t *testing.T) {
	status, _, errOut := run("frobnicate")
	if status != ExitUsage || !strings.Contains(errOut, "ugt recipe verify") || !strings.Contains(errOut, "ugt recipe validate") {
		t.Fatalf("status %d, usage %q", status, errOut)
	}
	if status, out, _ := run("version"); status != ExitOK || out != "ugt test\n" {
		t.Fatalf("version: %d %q", status, out)
	}
}

func TestPulls(t *testing.T) {
	p := pulls{}
	for _, v := range []string{"localhost:5001/x@sha256:1", "-api=ghcr.io/y@sha256:2"} {
		if err := p.Set(v); err != nil {
			t.Fatal(err)
		}
	}
	if p[""] != "localhost:5001/x@sha256:1" || p["-api"] != "ghcr.io/y@sha256:2" {
		t.Fatalf("pulls %v", p)
	}
	for _, bad := range []string{"api=ref", "-api="} {
		if err := (pulls{}).Set(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestVerifyNeedsOneFolder(t *testing.T) {
	if status, _, _ := run("recipe", "verify"); status != ExitUsage {
		t.Fatalf("status %d", status)
	}
}
