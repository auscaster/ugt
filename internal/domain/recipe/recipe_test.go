package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const examples = "../../../spec/recipe/v0/examples"

func TestValidExamples(t *testing.T) {
	for _, dir := range glob(t, "valid") {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			if err := Validate(os.DirFS(dir)); err != nil {
				t.Fatalf("expected valid, got:\n%v", err)
			}
		})
	}
}

func TestInvalidExamples(t *testing.T) {
	for _, dir := range glob(t, "invalid") {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			if err := Validate(os.DirFS(dir)); err == nil {
				t.Fatal("expected a violation, got none")
			}
		})
	}
}

// Undeclared access is invisible to the static rules: these validate, and
// only the resolved model refuses them (the docker adapter's contract test).
func TestUndeclaredExamplesValidateStatically(t *testing.T) {
	for _, dir := range glob(t, "undeclared") {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			if err := Validate(os.DirFS(dir)); err != nil {
				t.Fatalf("expected valid, got:\n%v", err)
			}
		})
	}
}

func TestRejectsNonJSON(t *testing.T) {
	if err := ValidateRecord([]byte("recipe_format: ugt-draft-4")); err == nil {
		t.Fatal("expected an error for non-JSON input")
	}
}

// A schema rule fails as "'not' failed"; the error must say which rule.
func TestNamesFailedRule(t *testing.T) {
	err := Validate(os.DirFS(filepath.Join(examples, "invalid", "reserved-setting-name")))
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
		{"variable inside a default", job + "  extra: {image: busybox, environment: {KEY: '${API_KEY:-${HOME}}'}}\n", "job", []string{"API_KEY"}, "${HOME} is neither"},
		{"comments are not read", "# reads $HOME at run time? no\n" + job, "job", nil, ""},
		{"named volume", job + "  extra: {image: busybox, volumes: ['data:/data']}\nvolumes: {data: {}}\n", "job", nil, ""},
		// access is not the static lint's: the resolved model judges it
		{"access is left to the resolved model", job + "  extra: {image: busybox, privileged: true, ports: ['80'], volumes: ['/etc:/etc'], extra_hosts: ['h:host-gateway']}\n", "job", nil, ""},
		{"long-form local port", job + "  extra: {image: busybox, ports: [{target: 80, host_ip: 127.0.0.1}]}\n", "job", nil, ""},
		{"not yaml", "services: [", "job", nil, "not valid YAML"},
		{"git build context", job + "  extra: {image: '${UGT_IMAGE}-x', build: 'https://github.com/a/b.git#0123456789abcdef0123456789abcdef01234567:sub'}\n", "job", nil, ""},
		{"build from the host", job + "  extra: {image: '${UGT_IMAGE}-x', build: {context: /}}\n", "job", nil, `builds from "/"`},
		{"build from the workspace", job + "  extra: {image: '${UGT_IMAGE}-x', build: .}\n", "job", nil, `builds from "."`},
		{"dockerfile outside", job + "  extra: {image: '${UGT_IMAGE}-x', build: {context: ./recipe, dockerfile: ../../Dockerfile}}\n", "job", nil, "outside its context"},
		{"additional context", job + "  extra: {image: '${UGT_IMAGE}-x', build: {context: ./recipe, additional_contexts: {src: /home}}}\n", "job", nil, "additional_contexts.src"},
		{"env file in recipe", job + "  extra: {image: busybox, env_file: ./recipe/app.env}\n", "job", nil, ""},
		{"env file on the host", job + "  extra: {image: busybox, env_file: [/etc/environment]}\n", "job", nil, `env_file "/etc/environment"`},
		{"extends", job + "  extra: {image: busybox, extends: {file: ../x.yaml, service: y}}\n", "job", nil, "extends another service"},
		{"include", "include: [../other.yaml]\n" + job, "job", nil, "includes other Compose files"},
		{"secret file on the host", job + "secrets: {key: {file: ~/.ssh/id_rsa}}\n", "job", nil, `secrets.key reads "~/.ssh/id_rsa"`},
		{"secret from the environment", job + "secrets: {key: {environment: HOME}}\n", "job", nil, "host's environment"},
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
