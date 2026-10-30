package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/access"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

// recorder answers docker commands from a script and records what ran.
type recorder struct {
	ran     []app.Command
	answers map[string]string // first argument -> stdout
}

func (r *recorder) Exec(_ context.Context, c app.Command) (int, error) {
	r.ran = append(r.ran, c)
	if out, ok := r.answers[c.Args[0]]; ok && c.Stdout != nil {
		fmt.Fprint(c.Stdout, out)
	}
	return 0, nil
}

func TestComposeCommands(t *testing.T) {
	r := &recorder{}
	e := New(r)
	p := app.Project{Name: "ugt-a-b-c-verify", Dir: "/ws", Files: []string{"recipe/compose.yaml", ".ugt/override.yaml"},
		Env: map[string]string{"UGT_IMAGE": "ugt/a/b/c:0123456789ab"}}
	ctx := context.Background()
	if _, err := e.Run(ctx, p, "check", nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Up(ctx, p, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Run(ctx, p, "task", []string{"sh", "-c", "ls outputs"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	run, up, exec := r.ran[0], r.ran[1], r.ran[2]
	if got := strings.Join(exec.Args[len(exec.Args)-5:], " "); got != "--entrypoint sh task -c ls outputs" {
		t.Errorf("a command replaces the entrypoint: %s", got)
	}
	want := "compose --progress quiet -f recipe/compose.yaml -f .ugt/override.yaml --project-directory /ws --profile check --profile sample run --rm -T check"
	if got := strings.Join(run.Args, " "); got != want {
		t.Errorf("run args\n got %s\nwant %s", got, want)
	}
	if strings.Contains(strings.Join(up.Args, " "), "--profile") {
		t.Errorf("up must start only the default services: %v", up.Args)
	}
	if run.Name != "docker" || run.Dir != "/ws" || run.Env["COMPOSE_PROJECT_NAME"] != "ugt-a-b-c-verify" || run.Env["UGT_IMAGE"] == "" {
		t.Errorf("command %+v", run)
	}
}

func TestInfo(t *testing.T) {
	r := &recorder{answers: map[string]string{"version": "29.4.0 arm64\n", "info": "8319410176 8\n"}}
	info, err := New(r).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "29.4.0" || info.Arch != "arm64" || info.MemoryBytes != 8319410176 || info.CPUs != 8 {
		t.Errorf("info %+v", info)
	}
}

// localExec runs commands on this machine, for the contract test.
type localExec struct{}

func (localExec) Exec(ctx context.Context, c app.Command) (int, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = c.Dir, c.Stdout, c.Stderr
	for k, v := range c.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if cmd.Env != nil {
		cmd.Env = append(os.Environ(), cmd.Env...)
	}
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}

// Contract: against a real engine, Info answers with an architecture UGT
// builds for. Skipped where no engine runs.
func TestRealEngineInfo(t *testing.T) {
	if testing.Short() {
		t.Skip("contract test needs an engine")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("no docker engine here")
	}
	info, err := New(localExec{}).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Arch != "amd64" && info.Arch != "arm64" {
		t.Errorf("engine arch %q", info.Arch)
	}
}

// Contract: the real Compose resolves defaults and paths, and the domain reads
// what it prints. Skipped where no engine runs.
func TestRealEngineConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("contract test needs an engine")
	}
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skip("no docker compose here")
	}
	dir := t.TempDir() // on macOS a symlink into /private: Compose must keep this spelling
	if err := os.MkdirAll(filepath.Join(dir, "recipe"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := `services:
  task:
    image: ${UGT_IMAGE}
    build: ./recipe
    privileged: ${P:-true}
    volumes: ["./inputs:/in:ro", "data:/data"]
    ports: ["127.0.0.1::8080"]
volumes:
  data:
    driver_opts: {type: none, o: bind, device: /srv}
`
	if err := os.WriteFile(filepath.Join(dir, "recipe", "compose.yaml"), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := New(localExec{}).Config(context.Background(), app.Project{
		Name: "ugt-contract", Dir: dir, Files: []string{"recipe/compose.yaml"}, Env: map[string]string{"UGT_IMAGE": "ugt/a/b/c:1", "P": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := m.Services["task"]
	if !task.Builds() || task.Image != "ugt/a/b/c:1" || !task.Privileged || task.Ports[0].HostIP != "127.0.0.1" {
		t.Errorf("task %+v", task)
	}
	if len(task.Mounts) != 2 || task.Mounts[0].Source != filepath.Join(dir, "inputs") || !task.Mounts[0].ReadOnly || task.Mounts[1].Source != "data" {
		t.Errorf("mounts %+v", task.Mounts)
	}
	if m.Volumes["data"].DriverOpts["device"] != "/srv" {
		t.Errorf("volumes %+v", m.Volumes)
	}
}

// Contract: on the real engine's resolved model, every undeclared-access
// example is refused for exactly the kind it hides, by defaults, variables
// and volume drivers included, and the valid examples declare what they get.
func TestRealEngineChecksAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("contract test needs an engine")
	}
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skip("no docker compose here")
	}
	const examples = "../../../spec/recipe/v0/examples"
	for _, set := range []string{"undeclared", "valid"} {
		dirs, err := filepath.Glob(filepath.Join(examples, set, "*"))
		if err != nil || len(dirs) == 0 {
			t.Fatalf("no %s examples: %v", set, err)
		}
		for _, dir := range dirs {
			t.Run(set+"/"+filepath.Base(dir), func(t *testing.T) {
				f, err := recipe.Load(os.DirFS(dir))
				if err != nil {
					t.Fatal(err)
				}
				var hides struct {
					Kind string `json:"x-undeclared"`
				}
				data, _ := os.ReadFile(filepath.Join(dir, recipe.RecordFile))
				if err := json.Unmarshal(data, &hides); err != nil {
					t.Fatal(err)
				}
				ws := t.TempDir()
				abs, _ := filepath.Abs(dir)
				if err := os.Symlink(abs, filepath.Join(ws, "recipe")); err != nil {
					t.Fatal(err)
				}
				env := map[string]string{"UGT_IMAGE": "ugt/a/b/c:1", "UGT_ARCH": "arm64"}
				for _, s := range f.Recipe.Settings {
					env[s.Name] = ""
				}
				m, err := New(localExec{}).Config(context.Background(), app.Project{Name: "ugt-contract", Dir: ws, Files: []string{"recipe/compose.yaml"}, Env: env})
				if err != nil {
					t.Fatal(err)
				}
				problems := access.Check(f.Recipe.Access, m, ws)
				switch {
				case hides.Kind == "" && len(problems) > 0:
					t.Errorf("problems %v", problems)
				case hides.Kind != "" && (len(problems) != 1 || !strings.Contains(problems[0], fmt.Sprintf("{\"kind\": %q", hides.Kind))):
					t.Errorf("hides %s, problems %v", hides.Kind, problems)
				}
			})
		}
	}
}

func TestImageDigest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	r := &recorder{answers: map[string]string{"buildx": digest + "\n"}}
	got, err := New(r).ImageDigest(context.Background(), "busybox:1.37")
	if err != nil || got != digest {
		t.Fatalf("ImageDigest = %q, %v", got, err)
	}
	if args := strings.Join(r.ran[0].Args, " "); args != "buildx imagetools inspect busybox:1.37 --format {{.Manifest.Digest}}" {
		t.Errorf("args %s", args)
	}
	if _, err := New(&recorder{answers: map[string]string{"buildx": "nope"}}).ImageDigest(context.Background(), "x"); err == nil {
		t.Error("a malformed digest was accepted")
	}
}
