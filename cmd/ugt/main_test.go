package main

import (
	"bytes"
	"strings"
	"testing"
)

const examples = "../../spec/recipe/v0/examples/"

func TestRecipeValidate(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		status int
		out    string
	}{
		{"valid", []string{"recipe", "validate", examples + "valid/openai-whisper.json"}, 0, "ok"},
		{"invalid", []string{"recipe", "validate", examples + "invalid/missing-check.json"}, 1, "invalid"},
		{"missing file", []string{"recipe", "validate", examples + "nope.json"}, 1, "invalid"},
		{"mixed", []string{"recipe", "validate", examples + "valid/openai-whisper.json", examples + "invalid/short-commit.json"}, 1, "invalid"},
		{"no files", []string{"recipe", "validate"}, 2, ""},
		{"unknown command", []string{"run"}, 2, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(c.args, &stdout, &stderr); got != c.status {
				t.Fatalf("status %d, want %d\nstdout: %s\nstderr: %s", got, c.status, stdout.String(), stderr.String())
			}
			if c.status == 2 && !strings.Contains(stderr.String(), "usage:") {
				t.Fatalf("expected usage on stderr, got %q", stderr.String())
			}
			if c.out != "" && !strings.Contains(stdout.String(), c.out) {
				t.Fatalf("expected %q in output, got %q", c.out, stdout.String())
			}
		})
	}
}
