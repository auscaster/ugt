// Package docker implements the Engine port with the docker CLI: Docker
// Engine with Compose and Buildx. The CLI is the contract Docker Desktop,
// OrbStack, Colima, a native engine and UGT's own WSL engine all share, and
// Compose has no stable embeddable runner.
package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/usegeekthing/ugt/internal/app"
	"github.com/usegeekthing/ugt/internal/domain/compose"
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
var allProfiles = []string{"--profile", "check", "--profile", "sample", "--profile", "command", "--profile", "mcp"}

// composeArgs builds a compose command line. Progress is quiet, so a step's
// log holds the program's own output rather than Compose's container events;
// builds pass "plain", whose progress is the build log.
func composeArgs(p app.Project, profiles bool, args ...string) []string {
	return composeArgsWith(p, profiles, "quiet", args...)
}

func composeArgsWith(p app.Project, profiles bool, progress string, args ...string) []string {
	out := []string{"compose", "--progress", progress}
	for _, f := range p.Files {
		out = append(out, "-f", f)
	}
	out = append(out, "--project-directory", p.Dir)
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
	if out, err := e.output(ctx, "", nil, "info", "--format", "{{.MemTotal}} {{.NCPU}}"); err == nil {
		if f := strings.Fields(string(out)); len(f) == 2 {
			info.MemoryBytes, _ = strconv.ParseInt(f[0], 10, 64)
			info.CPUs, _ = strconv.Atoi(f[1])
		}
	}
	return info, nil
}

// Config returns the resolved Compose model with every profile enabled.
func (e *Engine) Config(ctx context.Context, p app.Project) (compose.Model, error) {
	out, err := e.output(ctx, p.Dir, projectEnv(p), composeArgs(p, true, "config", "--format", "json")...)
	if err != nil {
		return compose.Model{}, err
	}
	return compose.Parse(out)
}

func (e *Engine) Build(ctx context.Context, p app.Project, log io.Writer) (int, error) {
	return e.stream(ctx, p, log, composeArgsWith(p, true, "plain", "build")...)
}

// Logs writes the last lines each of the project's containers logged, or
// only the named services' containers.
func (e *Engine) Logs(ctx context.Context, p app.Project, log io.Writer, services ...string) error {
	return check(e.stream(ctx, p, log, composeArgs(p, true, append([]string{"logs", "--no-color", "--tail", "60"}, services...)...)...))
}

func (e *Engine) Run(ctx context.Context, p app.Project, service string, command []string, log io.Writer) (int, error) {
	args := []string{"run", "--rm", "-T"}
	if len(command) > 0 {
		args = append(args, "--entrypoint", command[0], service)
		args = append(args, command[1:]...)
	} else {
		args = append(args, service)
	}
	return e.stream(ctx, p, log, composeArgs(p, true, args...)...)
}

// Attach runs a service for the user: their arguments after its entrypoint,
// their folder mounted at its own path as the working directory, and their
// terminal when they have one, or plain streams for an AI app.
func (e *Engine) Attach(ctx context.Context, p app.Project, service string, args []string, to app.Attached) (int, error) {
	run := []string{"run", "--rm", "--quiet-pull"}
	if !to.Terminal {
		run = append(run, "-T")
	}
	if to.Folder != "" {
		run = append(run, "--volume", to.Folder+":"+to.Folder, "--workdir", to.Folder)
	}
	run = append(append(run, service), args...)
	return e.runtime.Exec(ctx, app.Command{Name: "docker", Args: composeArgs(p, true, run...), Dir: p.Dir, Env: projectEnv(p),
		Stdin: to.Stdin, Stdout: to.Stdout, Stderr: to.Stderr})
}

func (e *Engine) Up(ctx context.Context, p app.Project, log io.Writer) (int, error) {
	// Wait for these running containers, not one-shot setup services, or a
	// replacement project with the same name. No recipe settings are needed.
	const wait = `ids=$(docker ps -q --filter "label=com.docker.compose.project=$1") || exit 1
[ -n "$ids" ] || exit 1
exec docker wait $ids`
	return e.runtime.Exec(ctx, app.Command{Name: "docker", Args: composeArgs(p, false, "up", "--detach", "--wait"),
		Dir: p.Dir, Env: projectEnv(p), Stdout: log, Stderr: log,
		Until: []string{"sh", "-ec", wait, "ugt", p.Name}})
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

func (e *Engine) Tag(ctx context.Context, source, target string) error {
	_, err := e.output(ctx, "", nil, "tag", source, target)
	return err
}

func (e *Engine) Running(ctx context.Context, p app.Project) (bool, error) {
	out, err := e.output(ctx, p.Dir, projectEnv(p), composeArgs(p, true, "ps", "--status", "running", "--quiet")...)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func (e *Engine) ImageExists(ctx context.Context, ref string) (bool, error) {
	exit, err := e.run(ctx, "", nil, []string{"image", "inspect", "--format", "{{.Id}}", ref}, io.Discard, io.Discard)
	return err == nil && exit == 0, err
}

func (e *Engine) RemoveImage(ctx context.Context, ref string) error {
	var stderr bytes.Buffer
	exit, err := e.run(ctx, "", nil, []string{"image", "rm", ref}, io.Discard, &stderr)
	if err != nil {
		return err
	}
	if exit != 0 && !strings.Contains(stderr.String(), "No such image") {
		return fmt.Errorf("docker image rm %s: %s", ref, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (e *Engine) Volumes(ctx context.Context, project string) ([]string, error) {
	out, err := e.output(ctx, "", nil, "volume", "ls", "--quiet", "--filter", "label=com.docker.compose.project="+project)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

func (e *Engine) RemoveVolume(ctx context.Context, name string) error {
	var stderr bytes.Buffer
	exit, err := e.run(ctx, "", nil, []string{"volume", "rm", name}, io.Discard, &stderr)
	if err != nil {
		return err
	}
	if exit != 0 && !strings.Contains(stderr.String(), "no such volume") {
		return fmt.Errorf("docker volume rm %s: %s", name, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// ImageDigest reads a reference's digest from its registry: the index digest
// for a multi-platform image, so the pin holds on every architecture.
func (e *Engine) ImageDigest(ctx context.Context, ref string) (string, error) {
	out, err := e.output(ctx, "", nil, "buildx", "imagetools", "inspect", ref, "--format", "{{.Manifest.Digest}}")
	if err != nil {
		return "", err
	}
	digest := strings.TrimSpace(string(out))
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		return "", fmt.Errorf("docker buildx imagetools inspect %s: unexpected digest %q", ref, digest)
	}
	return digest, nil
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
