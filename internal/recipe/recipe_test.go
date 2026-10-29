package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const examples = "../../spec/recipe/v0/examples"

func TestValidExamples(t *testing.T) {
	for _, dir := range glob(t, "valid") {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			if err := ValidateDir(dir); err != nil {
				t.Fatalf("expected valid, got:\n%v", err)
			}
		})
	}
}

func TestInvalidExamples(t *testing.T) {
	for _, dir := range glob(t, "invalid") {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			if err := ValidateDir(dir); err == nil {
				t.Fatal("expected a violation, got none")
			}
		})
	}
}

func TestRejectsNonJSON(t *testing.T) {
	if err := ValidateRecord([]byte("recipe_format: ugt-draft-2")); err == nil {
		t.Fatal("expected an error for non-JSON input")
	}
}

// A schema rule fails as "'not' failed"; the error must say which rule.
func TestNamesFailedRule(t *testing.T) {
	err := ValidateDir(filepath.Join(examples, "invalid", "reserved-setting-name"))
	if err == nil || !strings.Contains(err.Error(), "belong to the executor") {
		t.Fatalf("expected the rule's description, got:\n%v", err)
	}
}

func TestComposeRules(t *testing.T) {
	const job = `
services:
  task: {image: "${UGT_IMAGE}", build: {context: ./recipe}}
  check: {image: "${UGT_IMAGE}", profiles: [check]}
  sample: {image: "${UGT_IMAGE}", profiles: [sample]}
  sample-check: {image: "${UGT_IMAGE}", profiles: [sample]}
`
	cases := []struct {
		name, compose, kind string
		settings            []string
		want                string
	}{
		{"clean job", job, "job", nil, ""},
		{"service needs no task", strings.Replace(job, `  task: {image: "${UGT_IMAGE}", build: {context: ./recipe}}`, "  app: {image: busybox}", 1), "service", nil, ""},
		{"job needs a task", strings.Replace(job, `  task: {image: "${UGT_IMAGE}", build: {context: ./recipe}}`, "  app: {image: busybox}", 1), "job", nil, "services.task is missing"},
		{"escaped dollar is literal", job + "  extra: {image: busybox, command: [sh, -c, 'echo $$HOME']}\n", "job", nil, ""},
		{"bare variable", job + "  extra: {image: busybox, command: [sh, -c, 'echo $HOME']}\n", "job", nil, "${HOME} is neither"},
		{"declared setting", job + "  extra: {image: busybox, environment: {KEY: '${API_KEY}'}}\n", "job", []string{"API_KEY"}, ""},
		{"named volume", job + "  extra: {image: busybox, volumes: ['data:/data']}\nvolumes: {data: {}}\n", "job", nil, ""},
		{"long-form bind outside", job + "  extra: {image: busybox, volumes: [{type: bind, source: /etc, target: /etc}]}\n", "job", nil, `mounts "/etc"`},
		{"escaping bind", job + "  extra: {image: busybox, volumes: ['./outputs/../..:/x']}\n", "job", nil, "only ./inputs"},
		{"long-form local port", job + "  extra: {image: busybox, ports: [{target: 80, host_ip: 127.0.0.1}]}\n", "job", nil, ""},
		{"container port only", job + "  extra: {image: busybox, ports: ['80']}\n", "job", nil, "beyond 127.0.0.1"},
		{"not yaml", "services: [", "job", nil, "not valid YAML"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			problems := strings.Join(lintCompose([]byte(c.compose), c.kind, c.settings), "\n")
			if c.want == "" && problems != "" {
				t.Fatalf("expected no problems, got:\n%s", problems)
			}
			if !strings.Contains(problems, c.want) {
				t.Fatalf("expected a problem containing %q, got:\n%s", c.want, problems)
			}
		})
	}
}

func glob(t *testing.T, kind string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(examples, kind))
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(examples, kind, e.Name()))
		}
	}
	if len(dirs) == 0 {
		t.Fatalf("no %s examples found", kind)
	}
	return dirs
}
