package docker

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/app"
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
	want := "compose --progress quiet -f recipe/compose.yaml -f .ugt/override.yaml --project-directory . --profile check --profile sample run --rm -T check"
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

func TestParseConfig(t *testing.T) {
	cfg, err := parseConfig([]byte(`{"services": {
		"task": {"image": "ugt/a/b/c:1", "build": {"context": "/ws/recipe"}, "mem_limit": 3221225472},
		"check": {"image": "ugt/a/b/c:1", "profiles": ["check"]},
		"api": {"image": "x", "ports": [{"mode": "ingress", "host_ip": "127.0.0.1", "target": 3002, "protocol": "tcp"}],
			"deploy": {"resources": {"limits": {"memory": "1073741824"},
				"reservations": {"devices": [{"capabilities": ["gpu"]}]}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Services["task"].Builds || cfg.Services["check"].Builds || cfg.Services["check"].Profiles[0] != "check" {
		t.Errorf("config %+v", cfg)
	}
	if task, api := cfg.Services["task"], cfg.Services["api"]; task.MemoryBytes != 3<<30 || api.MemoryBytes != 1<<30 ||
		len(api.Ports) != 1 || api.Ports[0] != 3002 || !api.GPU || task.GPU {
		t.Errorf("resources: task %+v, api %+v", task, api)
	}
}

func TestInfo(t *testing.T) {
	r := &recorder{answers: map[string]string{"version": "29.4.0 arm64\n", "info": "8319410176\n"}}
	info, err := New(r).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "29.4.0" || info.Arch != "arm64" || info.MemoryBytes != 8319410176 {
		t.Errorf("info %+v", info)
	}
}

// localExec runs commands on this machine, for the contract test.
type localExec struct{}

func (localExec) Exec(ctx context.Context, c app.Command) (int, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Stdout, cmd.Stderr = c.Stdout, c.Stderr
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
