// Package docker implements the Engine port with the docker CLI: Docker
// Engine with Compose and Buildx. The CLI is the contract Docker Desktop,
// OrbStack, Colima, a native engine and UGT's own WSL engine all share, and
// Compose has no stable embeddable runner.
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/auscaster/ugt/internal/app"
)

// Execer runs programs in the runtime the engine lives in. Every runtime
// adapter provides it through the Runtime port.
type Execer interface {
	Exec(ctx context.Context, c app.Command) (exit int, err error)
}

// Engine drives Docker through an Execer.
type Engine struct {
	runtime Execer
}

// New returns an Engine that runs docker in runtime.
func New(runtime Execer) *Engine { return &Engine{runtime: runtime} }

// run runs one docker command.
func (e *Engine) run(ctx context.Context, dir string, env map[string]string, args []string, stdout, stderr io.Writer) (int, error) {
	return e.runtime.Exec(ctx, app.Command{Name: "docker", Args: args, Dir: dir, Env: env, Stdout: stdout, Stderr: stderr})
}

var _ app.Engine = (*Engine)(nil)

// optional profiles are enabled for everything except starting the default
// services, so their images build and they can be run by name.
var allProfiles = []string{"--profile", "check", "--profile", "sample"}

func composeArgs(p app.Project, profiles bool, args ...string) []string {
	out := []string{"compose"}
	for _, f := range p.Files {
		out = append(out, "-f", f)
	}
	out = append(out, "--project-directory", ".")
	if profiles {
		out = append(out, allProfiles...)
	}
	return append(out, args...)
}

func projectEnv(p app.Project) map[string]string {
	env := map[string]string{"COMPOSE_PROJECT_NAME": p.Name}
	for k, v := range p.Env {
		env[k] = v
	}
	return env
}

// output runs a command and returns its stdout, failing on a non-zero exit.
func (e *Engine) output(ctx context.Context, dir string, env map[string]string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	exit, err := e.run(ctx, dir, env, args, &stdout, &stderr)
	if err != nil {
		return nil, err
	}
	if exit != 0 {
		return nil, fmt.Errorf("docker %s: exit %d: %s", strings.Join(args, " "), exit, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// stream runs a command with all its output going to log.
func (e *Engine) stream(ctx context.Context, p app.Project, log io.Writer, args ...string) (int, error) {
	return e.run(ctx, p.Dir, projectEnv(p), args, log, log)
}

// Info reports the engine's version, CPU architecture and memory.
func (e *Engine) Info(ctx context.Context) (app.EngineInfo, error) {
	out, err := e.output(ctx, "", nil, "version", "--format", "{{.Server.Version}} {{.Server.Arch}}")
	if err != nil {
		return app.EngineInfo{}, err
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return app.EngineInfo{}, fmt.Errorf("unexpected docker version output %q", out)
	}
	info := app.EngineInfo{Version: fields[0], Arch: fields[1]}
	if mem, err := e.output(ctx, "", nil, "info", "--format", "{{.MemTotal}}"); err == nil {
		info.MemoryBytes, _ = strconv.ParseInt(strings.TrimSpace(string(mem)), 10, 64)
	}
	return info, nil
}

// Config returns the resolved configuration with every profile enabled.
func (e *Engine) Config(ctx context.Context, p app.Project) (app.ComposeConfig, error) {
	out, err := e.output(ctx, p.Dir, projectEnv(p), composeArgs(p, true, "config", "--format", "json")...)
	if err != nil {
		return app.ComposeConfig{}, err
	}
	return parseConfig(out)
}

func parseConfig(data []byte) (app.ComposeConfig, error) {
	var raw struct {
		Services map[string]struct {
			Image       string          `json:"image"`
			Build       json.RawMessage `json:"build"`
			Profiles    []string        `json:"profiles"`
			SecurityOpt []string        `json:"security_opt"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return app.ComposeConfig{}, fmt.Errorf("read compose config: %w", err)
	}
	cfg := app.ComposeConfig{Services: map[string]app.ComposeService{}}
	for name, s := range raw.Services {
		cfg.Services[name] = app.ComposeService{
			Image:       s.Image,
			Builds:      len(s.Build) > 0 && string(s.Build) != "null",
			Profiles:    s.Profiles,
			SecurityOpt: s.SecurityOpt,
		}
	}
	return cfg, nil
}

func (e *Engine) Build(ctx context.Context, p app.Project, log io.Writer) (int, error) {
	return e.stream(ctx, p, log, composeArgs(p, true, "build")...)
}

func (e *Engine) Run(ctx context.Context, p app.Project, service string, log io.Writer) (int, error) {
	return e.stream(ctx, p, log, composeArgs(p, true, "run", "--rm", "-T", service)...)
}

func (e *Engine) Up(ctx context.Context, p app.Project, log io.Writer) (int, error) {
	return e.stream(ctx, p, log, composeArgs(p, false, "up", "--detach", "--wait")...)
}

func (e *Engine) Stop(ctx context.Context, p app.Project, log io.Writer) error {
	return check(e.stream(ctx, p, log, composeArgs(p, true, "stop")...))
}

func (e *Engine) Down(ctx context.Context, p app.Project, volumes bool, log io.Writer) error {
	args := []string{"down", "--remove-orphans"}
	if volumes {
		args = append(args, "--volumes")
	}
	return check(e.stream(ctx, p, log, composeArgs(p, true, args...)...))
}

func (e *Engine) Pull(ctx context.Context, ref string, log io.Writer) error {
	return check(e.run(ctx, "", nil, []string{"pull", "--quiet", ref}, log, log))
}

func (e *Engine) Tag(ctx context.Context, source, target string) error {
	_, err := e.output(ctx, "", nil, "tag", source, target)
	return err
}

func check(exit int, err error) error {
	if err != nil {
		return err
	}
	if exit != 0 {
		return fmt.Errorf("exit %d", exit)
	}
	return nil
}
