package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/auscaster/ugt/internal/domain/recipe"
	"github.com/auscaster/ugt/recipes"
)

// Contract: an AI app starts ugt mcp, finds a recipe and runs it on a file the
// user dropped in their UGT folder, by its bare name, and the result lands
// there. It runs the real binary against the real engine in a fresh home
// folder. On a developer's machine it runs only when the recipe's image is
// already built, so it never builds; CI sets UGT_E2E_BUILD=1 to let it build,
// on any runtime, including UGT's WSL distro.
func TestMCPEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("contract test needs an engine")
	}
	const id = "earthtojake/text-to-cad/stl"
	sub, err := fs.Sub(recipes.FS, id)
	if err != nil {
		t.Fatal(err)
	}
	build := os.Getenv("UGT_E2E_BUILD") == "1"
	if !build {
		rev, err := recipe.Revision(sub)
		if err != nil {
			t.Fatal(err)
		}
		parsed, _ := recipe.ParseID(id)
		if exec.Command("docker", "info").Run() != nil {
			t.Skip("no docker engine here")
		}
		if exec.Command("docker", "image", "inspect", parsed.Image(rev)).Run() != nil {
			t.Skipf("%s is not built here, and this test builds only with UGT_E2E_BUILD=1", parsed.Image(rev))
		}
	}

	bin := filepath.Join(t.TempDir(), "ugt")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	home := t.TempDir()
	model, err := fs.ReadFile(sub, "check/sample.py")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "UGT"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "UGT", "stand.py"), model, 0o644); err != nil {
		t.Fatal(err)
	}

	// a fresh home and state, on every OS; docker keeps its own configuration
	realHome, _ := os.UserHomeDir()
	cmd := exec.Command(bin, "mcp")
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home,
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"), "LOCALAPPDATA="+filepath.Join(home, "AppData", "Local"),
		"DOCKER_CONFIG="+filepath.Join(realHome, ".docker"))
	limit := 5 * time.Minute
	if build {
		limit = 45 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
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
	found := call("ugt_find", map[string]any{"query": "earthtojake/text-to-cad"})
	if recipes, _ := found["recipes"].([]any); len(recipes) != 1 {
		t.Fatalf("find %v", found)
	}
	ran := call("ugt_run", map[string]any{"project": "https://github.com/earthtojake/text-to-cad", "inputs": []string{"stand.py"}})
	for ran["state"] == "running" {
		ran = call("ugt_status", map[string]any{"job": ran["job"], "wait_seconds": 30})
	}
	result, _ := ran["result"].(map[string]any)
	if ran["state"] != "succeeded" || result["passed"] != true {
		t.Fatalf("run %v", ran)
	}
	stl := filepath.Join(home, "UGT", "earthtojake", "text-to-cad", "stl", "phone_stand.stl")
	if info, err := os.Stat(stl); err != nil || info.Size() == 0 {
		t.Fatalf("no result at %s: %v", stl, err)
	}
}
