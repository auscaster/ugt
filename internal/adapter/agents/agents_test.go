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
