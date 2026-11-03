package agents

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/auscaster/ugt/internal/app"
)

var server = app.MCPServer{Command: "/opt/ugt/ugt", Args: []string{"mcp"}}

// fakeMachine has Claude Desktop with settings of its own, Claude Code on its
// path, and nothing else.
func fakeMachine(t *testing.T, goos string) (*Hosts, string, *[]string) {
	t.Helper()
	home := t.TempDir()
	var ran []string
	connected := map[string]bool{}
	h := &Hosts{
		GOOS: goos, Home: home,
		Getenv: func(k string) string {
			return map[string]string{"APPDATA": filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA": filepath.Join(home, "AppData", "Local")}[k]
		},
		LookPath: func(name string) (string, error) {
			if name == "claude" {
				return "/usr/local/bin/claude", nil
			}
			return "", errors.New("not found")
		},
		Run: func(_ context.Context, name string, args ...string) (int, error) {
			line := name + " " + strings.Join(args, " ")
			ran = append(ran, line)
			switch {
			case strings.HasPrefix(line, "claude mcp get ugt"):
				if !connected[name] {
					return 1, nil
				}
			case strings.HasPrefix(line, "claude mcp add"):
				connected[name] = true
			case strings.HasPrefix(line, "claude mcp remove"):
				connected[name] = false
			}
			return 0, nil
		},
	}
	return h, home, &ran
}

func desktopConfig(home, goos string) string {
	if goos == "windows" {
		return filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json")
	}
	return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
}

func TestConnectClaudeDesktop(t *testing.T) {
	for _, goos := range []string{"darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			h, home, _ := fakeMachine(t, goos)
			file := desktopConfig(home, goos)
			os.MkdirAll(filepath.Dir(file), 0o755)
			original := `{"preferences": {"theme": "dark"}, "mcpServers": {"other": {"command": "x"}}}`
			os.WriteFile(file, []byte(original), 0o644)

			found, err := h.Find(context.Background())
			if err != nil || len(found) != 2 || found[0].Name != "claude-desktop" || found[0].Connected || found[1].Name != "claude-code" {
				t.Fatalf("found %+v, %v", found, err)
			}
			if _, err := h.Connect(context.Background(), "claude-desktop", server); err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			data, _ := os.ReadFile(file)
			json.Unmarshal(data, &doc)
			servers := doc["mcpServers"].(map[string]any)
			if servers["other"] == nil || doc["preferences"] == nil {
				t.Errorf("connect lost the app's own settings: %s", data)
			}
			ugt := servers["ugt"].(map[string]any)
			if ugt["command"] != "/opt/ugt/ugt" || ugt["args"].([]any)[0] != "mcp" {
				t.Errorf("entry %v", ugt)
			}
			if backup, _ := os.ReadFile(file + ".before-ugt"); string(backup) != original {
				t.Errorf("no backup of the original: %q", backup)
			}
			found, _ = h.Find(context.Background())
			if !found[0].Connected {
				t.Error("not connected after connect")
			}
			if _, err := h.Disconnect(context.Background(), "claude-desktop"); err != nil {
				t.Fatal(err)
			}
			data, _ = os.ReadFile(file)
			if strings.Contains(string(data), `"ugt"`) || !strings.Contains(string(data), `"other"`) {
				t.Errorf("disconnect: %s", data)
			}
		})
	}
}

func TestStoreBuildOfClaudeDesktop(t *testing.T) {
	h, home, _ := fakeMachine(t, "windows")
	store := filepath.Join(home, "AppData", "Local", "Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Roaming", "Claude")
	os.MkdirAll(store, 0o755)
	if _, err := h.Connect(context.Background(), "claude-desktop", server); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store, "claude_desktop_config.json")); err != nil {
		t.Errorf("the Store build's configuration was not used: %v", err)
	}
}

func TestConnectByCommand(t *testing.T) {
	h, home, ran := fakeMachine(t, "darwin")
	h.Skill = fstest.MapFS{"SKILL.md": {Data: []byte("---\nname: ugt\n---\n")}, "references/recipe.md": {Data: []byte("# Recipe\n")}}
	if _, err := h.Connect(context.Background(), "claude-code", server); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(*ran, "\n"), "claude mcp add --scope user ugt -- /opt/ugt/ugt mcp") {
		t.Errorf("ran %q", *ran)
	}
	skill := filepath.Join(home, ".claude", "skills", "ugt")
	if _, err := os.Stat(filepath.Join(skill, "references", "recipe.md")); err != nil {
		t.Errorf("the skill was not installed: %v", err)
	}
	if _, err := h.Disconnect(context.Background(), "claude-code"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(skill); !os.IsNotExist(err) {
		t.Error("disconnect left the skill")
	}
}

func TestLeavesFilesWithCommentsAlone(t *testing.T) {
	h, home, _ := fakeMachine(t, "darwin")
	file := filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json")
	os.MkdirAll(filepath.Dir(file), 0o755)
	commented := "{\n  // my servers\n  \"servers\": {}\n}\n"
	os.WriteFile(file, []byte(commented), 0o644)
	if _, err := h.Connect(context.Background(), "vscode", server); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Fatalf("got %v", err)
	}
	if data, _ := os.ReadFile(file); string(data) != commented {
		t.Errorf("the file was changed: %s", data)
	}
	if _, err := h.Connect(context.Background(), "notepad", server); err == nil {
		t.Error("an unknown app was accepted")
	}
}

// An app with UGT's plugin has UGT already: connect adds no second server
// or skill there, removes the ones it added before the plugin came, and
// disconnect leaves the plugin to the app rather than failing.
func TestUGTsPluginConnectsAnApp(t *testing.T) {
	for _, app := range []string{"claude", "codex"} {
		t.Run(app, func(t *testing.T) {
			h, home, ran := fakeMachine(t, "darwin")
			h.Skill = fstest.MapFS{"SKILL.md": {Data: []byte("---\nname: ugt\n---\n")}}
			h.LookPath = func(name string) (string, error) { return "/usr/local/bin/" + name, nil }
			own := true // ugt connect added its server before the plugin came
			run := h.Run
			h.Run = func(ctx context.Context, name string, args ...string) (int, error) {
				line := name + " " + strings.Join(args, " ")
				switch {
				case strings.HasPrefix(line, name+" mcp get ugt"):
					*ran = append(*ran, line)
					if name == "codex" || own { // Codex finds the plugin's server by the same name
						return 0, nil
					}
					return 1, nil
				case strings.HasPrefix(line, name+" mcp remove ugt"):
					own = false
				}
				return run(ctx, name, args...)
			}
			h.Output = func(_ context.Context, name string, args ...string) ([]byte, error) {
				switch name + " " + strings.Join(args, " ") {
				case "claude plugin list --json":
					return []byte(`[{"id":"ugt@auscaster","enabled":true,"mcpServers":{"ugt":{"command":"ugt","args":["mcp"]}}},{"id":"other@x","enabled":true,"mcpServers":{"o":{"command":"other"}}}]`), nil
				case "codex plugin list --json":
					return []byte(`{"installed":[{"pluginId":"ugt@auscaster","name":"ugt","enabled":true}],"available":[]}`), nil
				case "codex mcp list --json":
					if own { // an entry of Codex's own hides the plugin's of the same name
						return []byte(`[{"name":"ugt","transport":{"type":"stdio","command":"/opt/ugt/ugt","args":["mcp"],"env":null}}]`), nil
					}
					return []byte(`[{"name":"ugt","transport":{"type":"stdio","command":"ugt","args":["mcp"],"env":{"PLUGIN_ROOT":"/p"}}}]`), nil
				}
				return nil, errors.New("unknown command")
			}
			skills := map[string]string{"claude": filepath.Join(home, ".claude", "skills", "ugt"), "codex": filepath.Join(home, ".codex", "skills", "ugt")}[app]
			os.MkdirAll(skills, 0o755)
			name := map[string]string{"claude": "claude-code", "codex": "codex"}[app]

			done, err := h.Connect(context.Background(), name, server)
			if err != nil || !strings.Contains(done, "has UGT's plugin") || !strings.Contains(done, "removed the ugt MCP server UGT had added") {
				t.Fatalf("connect: %q, %v", done, err)
			}
			if strings.Contains(strings.Join(*ran, "\n"), app+" mcp add") {
				t.Errorf("added a second server: %q", *ran)
			}
			if _, err := os.Stat(skills); !os.IsNotExist(err) {
				t.Error("left the skill beside the plugin's")
			}
			found, _ := h.Find(context.Background())
			for _, f := range found {
				if f.Name == name && (!f.Connected || !strings.Contains(f.Where, "UGT's plugin")) {
					t.Errorf("found %+v", f)
				}
			}
			if done, err := h.Disconnect(context.Background(), name); err != nil || !strings.Contains(done, "remove UGT's plugin") {
				t.Errorf("disconnect: %q, %v", done, err)
			}
		})
	}
}

// Claude Desktop unpacks each extension into its own folder in Claude
// Extensions; with UGT's there (any version's), Claude Desktop is connected
// through it, and the entry setup added before is removed.
func TestUGTsExtensionConnectsClaudeDesktop(t *testing.T) {
	h, home, _ := fakeMachine(t, "darwin")
	extensions := filepath.Join(home, "Library", "Application Support", "Claude", "Claude Extensions")
	other := filepath.Join(extensions, "local.mcpb.someone.other")
	os.MkdirAll(other, 0o755)
	os.WriteFile(filepath.Join(other, "manifest.json"), []byte(`{"manifest_version":"0.3","name":"other"}`), 0o644)
	if _, err := h.Connect(context.Background(), "claude-desktop", server); err != nil {
		t.Fatal(err)
	}
	if found, _ := h.Find(context.Background()); found[0].Where != desktopConfig(home, "darwin") {
		t.Fatalf("another extension counted as UGT's: %+v", found[0])
	}
	ours := filepath.Join(extensions, "local.mcpb.auscaster.ugt")
	os.MkdirAll(filepath.Join(ours, "server"), 0o755)
	os.WriteFile(filepath.Join(ours, "manifest.json"), []byte(`{"manifest_version":"0.3","name":"ugt"}`), 0o644)
	done, err := h.Connect(context.Background(), "claude-desktop", server)
	if err != nil || !strings.Contains(done, "has UGT's extension") {
		t.Fatalf("connect: %q, %v", done, err)
	}
	if data, _ := os.ReadFile(desktopConfig(home, "darwin")); strings.Contains(string(data), `"ugt"`) {
		t.Errorf("setup's entry stayed beside the extension: %s", data)
	}
	if found, _ := h.Find(context.Background()); !found[0].Connected || found[0].Where != "UGT's extension in Claude Desktop" {
		t.Errorf("found %+v", found[0])
	}
	os.RemoveAll(ours) // the user removes the extension
	if found, _ := h.Find(context.Background()); found[0].Connected {
		t.Error("connected through an extension that is gone")
	}
}
