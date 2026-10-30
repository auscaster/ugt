package compose

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"

	"github.com/auscaster/ugt/internal/domain/recipe"
)

const commit = "0123456789abcdef0123456789abcdef01234567"

// An original with most of what real Compose files do.
const original = `version: "3.8"
name: myapp
x-common: &common
  restart: unless-stopped
  build: ./web
  extra_hosts:
    - "host.docker.internal:host-gateway"
  environment:
    LOG_LEVEL: ${LOG_LEVEL:-info}
services:
  web:
    <<: *common
    image: myapp/web
    container_name: myapp-web
    ports:
      - "${WEB_PORT:-8080}:80"
      - 9000
    volumes:
      - ./data:/var/lib/web
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
      - /etc/localtime:/etc/localtime:ro
      - cache:/cache
    env_file: .env
    environment:
      API_KEY: ${API_KEY}
      DB_URL: postgres://app:${DB_PASSWORD:?set it}@db/app
  worker:
    image: myapp/web
    command: worker
  db:
    image: postgres:16
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    privileged: true
volumes:
  cache: {}
`

var pins = map[string]string{
	"postgres:16": "sha256:" + strings.Repeat("a", 64),
	ProbeImage:    "sha256:" + strings.Repeat("b", 64),
}

var source = Source{Repo: "acme/myapp", Commit: commit, Dir: "deploy", Name: "deploy/compose.yaml in acme/myapp"}

func TestImages(t *testing.T) {
	got, err := Images([]byte(original))
	if err != nil {
		t.Fatal(err)
	}
	// myapp/web is built by the file itself, so only the others need pins
	if want := []string{ProbeImage, "postgres:16"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Images = %q, want %q", got, want)
	}
}

func TestAdopt(t *testing.T) {
	a, err := Adopt([]byte(original), source, pins, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("adopted:\n%s", a.Compose)
	var doc struct {
		Name     *string                   `yaml:"name"`
		Version  *string                   `yaml:"version"`
		Services map[string]map[string]any `yaml:"services"`
		Volumes  map[string]any            `yaml:"volumes"`
	}
	if err := yaml.Unmarshal(a.Compose, &doc); err != nil {
		t.Fatalf("adopted file is not YAML: %v\n%s", err, a.Compose)
	}
	web, worker, db := doc.Services["web"], doc.Services["worker"], doc.Services["db"]
	expect := func(what string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %#v, want %#v\n%s", what, got, want, a.Compose)
		}
	}
	expect("name", doc.Name, (*string)(nil))
	expect("version", doc.Version, (*string)(nil))
	expect("web.container_name", web["container_name"], nil)
	expect("web.image", web["image"], "${UGT_IMAGE}")
	expect("web.platform", web["platform"], "linux/${UGT_ARCH}")
	// the build is rewritten in the anchor that defines it, not overridden
	expect("web.build", web["build"], map[string]any{"context": "https://github.com/acme/myapp.git#" + commit + ":deploy/web"})
	expect("web.extra_hosts", web["extra_hosts"], nil)
	if strings.Count(string(a.Compose), "build:") != 1 {
		t.Errorf("want one build, in the anchor:\n%s", a.Compose)
	}
	expect("worker.image", worker["image"], "${UGT_IMAGE}")
	expect("db.image", db["image"], "postgres:16@"+pins["postgres:16"])
	expect("web.ports", web["ports"], []any{"127.0.0.1::80", "127.0.0.1::9000"})
	expect("web.volumes", web["volumes"], []any{
		"data:/var/lib/web",
		"./recipe/upstream/deploy/nginx.conf:/etc/nginx/nginx.conf:ro",
		"cache:/cache",
	})
	expect("web.env_file", web["env_file"], "./recipe/upstream/deploy/.env")
	expect("volumes", doc.Volumes, map[string]any{"cache": map[string]any{}, "data": map[string]any{}})
	expect("fetch", a.Fetch, []Fetch{
		{From: "deploy/nginx.conf", To: "upstream/deploy/nginx.conf"},
		{From: "deploy/.env", To: "upstream/deploy/.env"},
	})
	expect("kind", a.Kind, recipe.KindService)
	expect("check", doc.Services["check"]["command"], []any{"wget", "-q", "-O", "/dev/null", "http://web:80/"})
	if _, ok := doc.Services["task"]; ok {
		t.Error("a service recipe gained a task placeholder")
	}
	for _, name := range []string{"sample", "sample-check"} {
		cmd, _ := doc.Services[name]["command"].([]any)
		if len(cmd) != 3 || !strings.Contains(cmd[2].(string), recipe.ScaffoldMark) {
			t.Errorf("%s is not a placeholder: %v", name, doc.Services[name])
		}
	}
	// a default is fixed in place; ${NAME} alone is optional; only :? is required
	if !strings.Contains(string(a.Compose), "LOG_LEVEL: info") {
		t.Errorf("LOG_LEVEL was not fixed at its default:\n%s", a.Compose)
	}
	expect("settings", a.Settings, []recipe.Setting{
		{Name: "API_KEY", Kind: "secret", What: "Read by the original Compose file as ${API_KEY}; empty when not set"},
		{Name: "DB_PASSWORD", Kind: "secret", Required: true, What: "Read by the original Compose file as ${DB_PASSWORD}"},
	})
	expect("builds", a.Builds, []string{"web"})
	problems := strings.Join(a.Problems, "\n")
	for _, want := range []string{"engine's socket", "services.db is privileged"} {
		if !strings.Contains(problems, want) {
			t.Errorf("problems lack %q:\n%s", want, problems)
		}
	}
	if len(a.Problems) != 2 {
		t.Errorf("want exactly 2 problems, got:\n%s", problems)
	}
	if !strings.HasPrefix(string(a.Compose), "# Adapted by ugt recipe new from deploy/compose.yaml in acme/myapp, at commit\n# "+commit) {
		t.Errorf("no provenance header:\n%s", a.Compose)
	}
	// the anchor survives and still carries the shared environment
	if !strings.Contains(string(a.Compose), "&common") || !strings.Contains(string(a.Compose), "<<: *common") {
		t.Errorf("anchors were lost:\n%s", a.Compose)
	}
}

// A clean original adopts to a recipe that already keeps every rule, with
// only its placeholders left to write.
func TestAdoptedRecipeIsValid(t *testing.T) {
	clean := strings.NewReplacer(
		"      - /var/run/docker.sock:/var/run/docker.sock\n", "      - pgdata:/var/lib/postgresql/data\n",
		"    privileged: true\n", "",
		"  cache: {}\n", "  cache: {}\n  pgdata: {}\n",
	).Replace(original)
	a, err := Adopt([]byte(clean), source, pins, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Problems) > 0 {
		t.Fatalf("unexpected problems: %q", a.Problems)
	}
	id, _ := recipe.ParseID("acme/myapp/site")
	files, err := recipe.Draft{
		ID: id, Repo: "https://github.com/acme/myapp", Commit: commit, What: "Run the site",
		Kind: a.Kind, Licence: "MIT", Compose: a.Compose, Source: source.Name, Settings: a.Settings,
	}.Files()
	if err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{}
	for name, data := range files {
		fsys[name] = &fstest.MapFile{Data: data}
	}
	if err := recipe.Validate(fsys); err != nil {
		t.Fatalf("adopted recipe is invalid: %v\n%s\n%s", err, files[recipe.RecordFile], a.Compose)
	}
}

func TestAdoptLocalFileCannotPinBuilds(t *testing.T) {
	a, err := Adopt([]byte("services:\n  app:\n    build: .\n"), Source{Name: "compose.yaml"}, pins, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Problems) != 1 || !strings.Contains(a.Problems[0], "on this machine") {
		t.Fatalf("problems = %q", a.Problems)
	}
	if a.Kind != recipe.KindJob {
		t.Errorf("kind = %s; nothing publishes a port", a.Kind)
	}
}
