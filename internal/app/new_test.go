package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/domain/recipe"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

func authoringApp(t *testing.T, gh *fakeGitHub) (*App, string) {
	t.Helper()
	root := t.TempDir()
	return &App{
		Runtime: &fakeRuntime{root: filepath.ToSlash(root)},
		Engine:  &fakeEngine{},
		Disk:    osDisk{},
		GitHub:  gh,
		Layout:  workspace.Layout{Home: filepath.Join(root, "home"), State: filepath.Join(root, "state"), Workspaces: filepath.ToSlash(root)},
		Now:     fixedClock(),
	}, root
}

var whisper = Repo{Owner: "openai", Name: "whisper", DefaultBranch: "main", Licence: "MIT", Language: "Python"}

func TestNewBlank(t *testing.T) {
	a, root := authoringApp(t, &fakeGitHub{
		repos: map[string]Repo{"openai/whisper": whisper},
		files: map[string]string{"openai/whisper/README.md": "# whisper"},
	})
	res, err := a.New(context.Background(), NewOptions{Project: "https://github.com/openai/whisper", Task: "subtitles"})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "state", "recipes", "openai", "whisper", "subtitles")
	if res.Dir != want || res.Recipe != "openai/whisper/subtitles" || res.Kind != recipe.KindJob || res.Adopted != "" {
		t.Fatalf("result %+v", res)
	}
	if strings.Join(res.Files, " ") != "Dockerfile compose.yaml recipe.json" {
		t.Errorf("files %v", res.Files)
	}
	dockerfile, _ := os.ReadFile(filepath.Join(want, "Dockerfile"))
	if !strings.Contains(string(dockerfile), "ADD https://github.com/openai/whisper.git#"+fakeCommit+" /src") ||
		!strings.Contains(string(dockerfile), "FROM python:") {
		t.Errorf("Dockerfile:\n%s", dockerfile)
	}
	f, err := recipe.Load(os.DirFS(want))
	if err != nil {
		t.Fatalf("a blank recipe must be valid: %v", err)
	}
	if f.Recipe.Task != "Subtitles from openai/whisper" || f.Recipe.Licence != "MIT" || f.Recipe.Commit != fakeCommit {
		t.Errorf("recipe %+v", f.Recipe)
	}
	if next := strings.Join(res.Next, "\n"); !strings.Contains(next, "Write the task, check, sample and sample-check services") ||
		!strings.Contains(next, "Finish the Dockerfile") {
		t.Errorf("next:\n%s", next)
	}
	// a second New never overwrites the author's work
	if _, err := a.New(context.Background(), NewOptions{Project: "openai/whisper", Task: "subtitles"}); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Errorf("second New: %v", err)
	}
}

func TestNewAdoptsTheProjectsCompose(t *testing.T) {
	a, root := authoringApp(t, &fakeGitHub{
		repos: map[string]Repo{"acme/site": {Owner: "acme", Name: "site", DefaultBranch: "main", Licence: "AGPL-3.0"}},
		files: map[string]string{
			"acme/site/docker-compose.yml": `services:
  web:
    build: .
    ports: ["8080:80"]
    volumes:
      - ./conf/site.conf:/etc/site.conf:ro
      - ./templates:/srv/templates:ro
    env_file: .env
    environment:
      SECRET_KEY: ${SECRET_KEY}
  db:
    image: postgres:16
`,
			"acme/site/conf/site.conf":         "listen 80;",
			"acme/site/templates/index.html":   "<h1>hi</h1>",
			"acme/site/templates/mail/welcome": "hello",
		},
	})
	dir := filepath.Join(root, "work", "site")
	res, err := a.New(context.Background(), NewOptions{Project: "acme/site", Task: "site", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.Adopted != "docker-compose.yml in acme/site" || res.Kind != recipe.KindService {
		t.Fatalf("result %+v", res)
	}
	for _, f := range []string{"NOTICE", "upstream/conf/site.conf", "upstream/templates/index.html", "upstream/templates/mail/welcome"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	// what each published port gives is the author's to say, and .env is not
	// in the repository: the problems left
	if len(res.Problems) != 2 || !strings.Contains(res.Problems[0], "provides a page") || !strings.Contains(res.Problems[1], ".env") {
		t.Fatalf("problems %q", res.Problems)
	}
	data, _ := os.ReadFile(filepath.Join(dir, recipe.RecordFile))
	for _, want := range []string{`"packaging": "adapted"`, `"adapted_from": "https://github.com/acme/site/blob/` + fakeCommit + `/docker-compose.yml"`, `"name": "SECRET_KEY"`, `"kind": "secret"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("recipe.json lacks %s:\n%s", want, data)
		}
	}
	// once the problems are resolved the recipe keeps every rule
	compose, _ := os.ReadFile(filepath.Join(dir, recipe.ComposeFile))
	fixed := strings.Replace(string(compose), "    env_file: ./recipe/upstream/.env\n", "", 1)
	if err := os.WriteFile(filepath.Join(dir, recipe.ComposeFile), []byte(fixed), 0o644); err != nil {
		t.Fatal(err)
	}
	said := strings.ReplaceAll(string(data), recipe.ScaffoldMark+" say what the user gets on this port", "the site, in your browser")
	if err := os.WriteFile(filepath.Join(dir, recipe.RecordFile), []byte(said), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.Validate(dir); err != nil {
		t.Fatalf("resolved recipe is invalid: %v\n%s", err, fixed)
	}
}

func TestNewAdoptsFromALink(t *testing.T) {
	a, _ := authoringApp(t, &fakeGitHub{
		repos: map[string]Repo{"openai/whisper": whisper},
		files: map[string]string{"docker/awesome-compose/nginx/compose.yaml": "services:\n  web:\n    image: nginx:1.27\n    ports: [\"80\"]\n"},
	})
	res, err := a.New(context.Background(), NewOptions{
		Project: "openai/whisper", Task: "web",
		From: "https://github.com/docker/awesome-compose/blob/master/nginx/compose.yaml",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Adopted != "nginx/compose.yaml in docker/awesome-compose" || len(res.Problems) != 1 {
		t.Fatalf("result %+v", res)
	}
	// its published port is now a page the recipe provides, which UGT publishes
	data, _ := os.ReadFile(filepath.Join(res.Dir, recipe.RecordFile))
	compose, _ := os.ReadFile(filepath.Join(res.Dir, recipe.ComposeFile))
	if !strings.Contains(string(data), `"port": 80`) || strings.Contains(string(compose), "ports") {
		t.Errorf("the port was not moved into provides:\n%s\n%s", data, compose)
	}
}

func TestNewRejects(t *testing.T) {
	a, _ := authoringApp(t, &fakeGitHub{repos: map[string]Repo{"openai/whisper": whisper}})
	for _, c := range []struct {
		opts NewOptions
		want string
	}{
		{NewOptions{Project: "openai", Task: "x"}, "not a GitHub project"},
		{NewOptions{Project: "openai/whisper", Task: "Bad_Task"}, "the task is lowercase"},
		{NewOptions{Project: "openai/whisper", Task: "x", Kind: "daemon"}, "neither job nor service"},
		{NewOptions{Project: "no/such", Task: "x"}, "not found"},
	} {
		if _, err := a.New(context.Background(), c.opts); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: got %v, want %q", c.opts, err, c.want)
		}
	}
}
