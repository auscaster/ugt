package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Contract: an AI app starts ugt mcp, finds a recipe and runs it on a file the
// user dropped in their UGT folder, by its bare name, and the result lands
// there. It runs the real binary against the real engine in a fresh home
// folder, on any runtime, including UGT's WSL distro, with a fixture recipe
// as the only one in the published catalogue: an unprivileged busybox job,
// which pulls a small public image and builds nothing.
func TestMCPEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("contract test needs an engine")
	}
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" {
		t.Skip("native integration requires a disposable GitHub-hosted runner")
	}
	if exec.Command("docker", "info").Run() != nil {
		t.Skip("no docker engine here")
	}
	catalogueDir := t.TempDir()
	recipeDir := filepath.Join(catalogueDir, "mirror", "busybox", "greeting")
	if err := os.CopyFS(recipeDir, os.DirFS("../../.github/ci/non-root")); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(t.TempDir(), "ugt")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "UGT"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "UGT", "name.txt"), []byte("MCP\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// State and Docker configuration are disposable. Never inherit an account's
	// Docker credential helper merely because HOME is temporary.
	cmd := exec.Command(bin, "mcp")
	cmd.Env = append(os.Environ(), "UGT_CATALOGUE="+catalogueDir, "HOME="+home, "USERPROFILE="+home,
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"), "LOCALAPPDATA="+filepath.Join(home, "AppData", "Local"),
		"DOCKER_CONFIG="+filepath.Join(home, ".docker"))
	if runtime.GOOS == "darwin" {
		// the engine docker reaches: UGT's own machine needs a signed ugt and
		// its own setup in the fresh home, which the machine's own proof covers
		cmd.Env = append(cmd.Env, "UGT_RUNTIME=host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "contract"}, nil).Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(res.StructuredContent)
		var out map[string]any
		_ = json.Unmarshal(data, &out)
		if res.IsError {
			t.Fatalf("%s failed: %+v", name, res.Content)
		}
		return out
	}
	found := call("ugt_find", map[string]any{"query": "mirror/busybox"})
	if recipes, _ := found["recipes"].([]any); len(recipes) != 1 {
		t.Fatalf("find %v", found)
	}
	ran := call("ugt_run", map[string]any{"project": "https://github.com/mirror/busybox", "inputs": []string{"name.txt"}})
	for ran["state"] == "running" {
		ran = call("ugt_status", map[string]any{"job": ran["job"], "wait_seconds": 30})
	}
	result, _ := ran["result"].(map[string]any)
	if ran["state"] != "succeeded" || result["passed"] != true {
		t.Fatalf("run %v", ran)
	}
	greeting, err := os.ReadFile(filepath.Join(home, "UGT", "mirror", "busybox", "greeting", "greeting.txt"))
	if err != nil || string(greeting) != "Hello, MCP\n" {
		t.Fatalf("result %q, %v", greeting, err)
	}
}
