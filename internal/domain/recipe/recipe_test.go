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
	if err := ValidateRecord([]byte("recipe_format: ugt-draft-5")); err == nil {
		t.Fatal("expected an error for non-JSON input")
	}
}

// A recipe in a newer format asks for a newer UGT, not a fix to the recipe.
func TestNewerFormatAsksForAnUpdate(t *testing.T) {
	err := ValidateRecord([]byte(`{"recipe_format": "ugt-draft-12"}`))
	if err == nil || !strings.Contains(err.Error(), "run ugt update") {
		t.Fatalf("got %v", err)
	}
	err = ValidateRecord([]byte(`{"recipe_format": "ugt-draft-2"}`))
	if err == nil || strings.Contains(err.Error(), "ugt update") || !strings.Contains(err.Error(), "this UGT reads "+Format) {
		t.Fatalf("got %v", err)
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
	out := []File{{Path: "outputs/x", What: "x"}}
	plain := Recipe{Kind: KindJob, Outputs: out}
	with := func(settings ...string) Recipe {
		r := plain
		for _, s := range settings {
			r.Settings = append(r.Settings, Setting{Name: s})
		}
		return r
	}
	app := strings.Replace(job, `  task: {image: "${UGT_IMAGE}", build: {context: ./recipe}}`, "  app: {image: busybox}", 1)
	page := Provides{Pages: []Address{{Service: "app", Port: 8080, What: "w"}}}
	service := Recipe{Kind: KindService, Provides: page}
	cli := Recipe{Kind: KindJob, Provides: Provides{Commands: []Command{{Name: "jq", Service: "cli", What: "w"}}}}
	cases := []struct {
		name, compose string
		r             Recipe
		want          string
	}{
		{"clean job", job, plain, ""},
		{"service needs no task", app, service, ""},
		{"job needs a task", app, plain, "services.task is missing"},
		{"escaped dollar is literal", job + "  extra: {image: busybox, command: [sh, -c, 'echo $$HOME']}\n", plain, ""},
		{"bare variable", job + "  extra: {image: busybox, command: [sh, -c, 'echo $HOME']}\n", plain, "${HOME} is neither"},
		{"declared setting", job + "  extra: {image: busybox, environment: {KEY: '${API_KEY}'}}\n", with("API_KEY"), ""},
		{"variable inside a default", job + "  extra: {image: busybox, environment: {KEY: '${API_KEY:-${HOME}}'}}\n", with("API_KEY"), "${HOME} is neither"},
		{"comments are not read", "# reads $HOME at run time? no\n" + job, plain, ""},
		{"named volume", job + "  extra: {image: busybox, volumes: ['data:/data']}\nvolumes: {data: {}}\n", plain, ""},
		// access is not the static lint's: the resolved model judges it
		{"access is left to the resolved model", job + "  extra: {image: busybox, privileged: true, ports: ['80'], volumes: ['/etc:/etc'], extra_hosts: ['h:host-gateway']}\n", plain, ""},
		{"long-form local port", job + "  extra: {image: busybox, ports: [{target: 80, host_ip: 127.0.0.1}]}\n", plain, ""},
		{"not yaml", "services: [", plain, "not valid YAML"},
		{"git build context", job + "  extra: {image: '${UGT_IMAGE}-x', build: 'https://github.com/a/b.git#0123456789abcdef0123456789abcdef01234567:sub'}\n", plain, ""},
		{"build from the host", job + "  extra: {image: '${UGT_IMAGE}-x', build: {context: /}}\n", plain, `builds from "/"`},
		{"build from the workspace", job + "  extra: {image: '${UGT_IMAGE}-x', build: .}\n", plain, `builds from "."`},
		{"dockerfile outside", job + "  extra: {image: '${UGT_IMAGE}-x', build: {context: ./recipe, dockerfile: ../../Dockerfile}}\n", plain, "outside its context"},
		{"additional context", job + "  extra: {image: '${UGT_IMAGE}-x', build: {context: ./recipe, additional_contexts: {src: /home}}}\n", plain, "additional_contexts.src"},
		{"env file in recipe", job + "  extra: {image: busybox, env_file: ./recipe/app.env}\n", plain, ""},
		{"env file on the host", job + "  extra: {image: busybox, env_file: [/etc/environment]}\n", plain, `env_file "/etc/environment"`},
		{"extends", job + "  extra: {image: busybox, extends: {file: ../x.yaml, service: y}}\n", plain, "extends another service"},
		{"include", "include: [../other.yaml]\n" + job, plain, "includes other Compose files"},
		{"secret file on the host", job + "secrets: {key: {file: ~/.ssh/id_rsa}}\n", plain, `secrets.key reads "~/.ssh/id_rsa"`},
		{"secret from the environment", job + "secrets: {key: {environment: HOME}}\n", plain, "host's environment"},
		{"service has no task", job + "  app: {image: busybox}\n", service, "services.task must not be"},
		{"page in a job", job + "  app: {image: busybox}\n", Recipe{Kind: KindJob, Outputs: out, Provides: page}, "only a service recipe provides pages"},
		{"service delivers no outputs", app, Recipe{Kind: KindService, Outputs: out, Provides: page}, "outputs are what a job delivers"},
		{"provided port published", strings.Replace(app, "app: {image: busybox}", "app: {image: busybox, ports: ['127.0.0.1::8080']}", 1), service, "remove it from ports"},
		{"provided port of no service", job + "  web: {image: busybox}\n", Recipe{Kind: KindJob, Outputs: out, Provides: Provides{Endpoints: []Address{{Service: "api", Port: 80, What: "w"}}}}, "compose.yaml does not have"},
		{"command", job + "  cli: {image: busybox, profiles: [command]}\n", cli, ""},
		{"command without its profile", job + "  cli: {image: busybox}\n", cli, "needs profiles: [command]"},
		{"mcp server", job + "  mcp: {image: busybox, profiles: [mcp]}\n", Recipe{Kind: KindJob, Provides: Provides{MCPServers: []MCPServer{{Service: "mcp", What: "w"}}}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			problems := strings.Join(lintCompose([]byte(c.compose), c.r), "\n")
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
