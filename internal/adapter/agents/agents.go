// Package agents implements the AgentHosts port: it finds the AI apps on this
// machine and connects UGT's MCP server to each through that app's own
// documented mechanism. Claude Desktop, Cursor and VS Code read a JSON file;
// Claude Code and Codex have a command for it. A file is backed up once
// before UGT first changes it, and nothing else in it is touched.
package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/auscaster/ugt/internal/app"
)

// Name is the server's name in every app's configuration.
const Name = "ugt"

// Hosts finds and connects AI apps. Its fields locate the machine's folders
// and run commands; tests point them elsewhere.
type Hosts struct {
	GOOS     string
	Home     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Run      func(ctx context.Context, name string, args ...string) (int, error)
}

var _ app.AgentHosts = (*Hosts)(nil)

// New returns the hosts on this machine.
func New(goos, home string) *Hosts {
	return &Hosts{GOOS: goos, Home: home, Getenv: os.Getenv, LookPath: exec.LookPath, Run: run}
}

// host is one AI app and how it is connected.
type host struct {
	name, title string
	file        string // its MCP configuration file, for apps configured by file
	key         string // the object in that file that holds servers
	vscode      bool   // VS Code's entries name their transport
	command     string // its command line, for apps configured by command
}

func (h *Hosts) hosts() []host {
	appData, localAppData := h.Getenv("APPDATA"), h.Getenv("LOCALAPPDATA")
	config := filepath.Join(h.Home, ".config")
	if x := h.Getenv("XDG_CONFIG_HOME"); x != "" {
		config = x
	}
	var desktop, vscode string
	switch h.GOOS {
	case "darwin":
		desktop = filepath.Join(h.Home, "Library", "Application Support", "Claude")
		vscode = filepath.Join(h.Home, "Library", "Application Support", "Code", "User")
	case "windows":
		desktop = filepath.Join(appData, "Claude")
		// the Microsoft Store build keeps its configuration in its package
		if matches, _ := filepath.Glob(filepath.Join(localAppData, "Packages", "Claude_*", "LocalCache", "Roaming", "Claude")); len(matches) > 0 {
			desktop = matches[0]
		}
		vscode = filepath.Join(appData, "Code", "User")
	default:
		desktop = filepath.Join(config, "Claude")
		vscode = filepath.Join(config, "Code", "User")
	}
	return []host{
		{name: "claude-desktop", title: "Claude Desktop", file: filepath.Join(desktop, "claude_desktop_config.json"), key: "mcpServers"},
		{name: "claude-code", title: "Claude Code", command: "claude"},
		{name: "codex", title: "Codex", command: "codex"},
		{name: "cursor", title: "Cursor", file: filepath.Join(h.Home, ".cursor", "mcp.json"), key: "mcpServers"},
		{name: "vscode", title: "VS Code", file: filepath.Join(vscode, "mcp.json"), key: "servers", vscode: true},
	}
}

// Find lists the AI apps installed here and whether UGT is connected to each.
func (h *Hosts) Find(ctx context.Context) ([]app.AgentHost, error) {
	var out []app.AgentHost
	for _, x := range h.hosts() {
		installed, connected, err := h.state(ctx, x)
		if err != nil {
			return nil, err
		}
		if installed {
			where := x.file
			if where == "" {
				where = "the " + x.command + " command"
			}
			out = append(out, app.AgentHost{Name: x.name, Title: x.title, Connected: connected, Where: where})
		}
	}
	return out, nil
}

func (h *Hosts) state(ctx context.Context, x host) (installed, connected bool, err error) {
	if x.command != "" {
		if _, err := h.LookPath(x.command); err != nil {
			return false, false, nil
		}
		exit, err := h.Run(ctx, x.command, "mcp", "get", Name)
		return true, err == nil && exit == 0, nil
	}
	if _, err := os.Stat(filepath.Dir(x.file)); err != nil {
		return false, false, nil
	}
	doc, err := readConfig(x.file)
	if err != nil {
		return true, false, nil // a file UGT cannot read is not connected
	}
	servers, _ := doc[x.key].(map[string]any)
	_, connected = servers[Name]
	return true, connected, nil
}

// Connect adds UGT's server to an app and says what changed. Restarting the
// app makes it load the server.
func (h *Hosts) Connect(ctx context.Context, name string, s app.MCPServer) (string, error) {
	x, err := h.find(name)
	if err != nil {
		return "", err
	}
	if x.command != "" {
		_, _ = h.Run(ctx, x.command, "mcp", "remove", Name) // replaces an older entry
		args := []string{"mcp", "add"}
		if x.command == "claude" {
			args = append(args, "--scope", "user")
		}
		args = append(append(args, Name, "--", s.Command), s.Args...)
		if exit, err := h.Run(ctx, x.command, args...); err != nil || exit != 0 {
			return "", fmt.Errorf("%s %s failed (exit %d): %v", x.command, strings.Join(args, " "), exit, err)
		}
		return fmt.Sprintf("added the %s MCP server with %s mcp add", Name, x.command), nil
	}
	doc, err := readConfig(x.file)
	if err != nil {
		return "", fmt.Errorf("%s: %w; add UGT by hand: %q under %q, running %s %s", x.file, err, Name, x.key, s.Command, strings.Join(s.Args, " "))
	}
	entry := map[string]any{"command": s.Command, "args": s.Args}
	if x.vscode {
		entry["type"] = "stdio"
	}
	servers, _ := doc[x.key].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[Name] = entry
	doc[x.key] = servers
	if err := writeConfig(x.file, doc); err != nil {
		return "", err
	}
	return "added the " + Name + " MCP server to " + x.file + "; restart " + x.title + " to load it", nil
}

// Disconnect removes UGT's server from an app.
func (h *Hosts) Disconnect(ctx context.Context, name string) (string, error) {
	x, err := h.find(name)
	if err != nil {
		return "", err
	}
	if x.command != "" {
		args := []string{"mcp", "remove", Name}
		if x.command == "claude" {
			args = append(args, "--scope", "user")
		}
		if exit, err := h.Run(ctx, x.command, args...); err != nil || exit != 0 {
			return "", fmt.Errorf("%s %s failed (exit %d): %v", x.command, strings.Join(args, " "), exit, err)
		}
		return "removed the " + Name + " MCP server from " + x.title, nil
	}
	doc, err := readConfig(x.file)
	if err != nil {
		return "", fmt.Errorf("%s: %w", x.file, err)
	}
	servers, _ := doc[x.key].(map[string]any)
	if _, ok := servers[Name]; !ok {
		return x.title + " was not connected", nil
	}
	delete(servers, Name)
	if err := writeConfig(x.file, doc); err != nil {
		return "", err
	}
	return "removed the " + Name + " MCP server from " + x.file, nil
}

func (h *Hosts) find(name string) (host, error) {
	var names []string
	for _, x := range h.hosts() {
		if x.name == name {
			return x, nil
		}
		names = append(names, x.name)
	}
	return host{}, fmt.Errorf("UGT does not know the AI app %q; it knows %s", name, strings.Join(names, ", "))
}

// readConfig reads a JSON configuration file; one not there yet is empty.
// A file with comments is refused rather than rewritten without them.
func readConfig(file string) (map[string]any, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	doc := map[string]any{}
	if len(bytes.TrimSpace(data)) == 0 {
		return doc, nil
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("it is not plain JSON (%v), so UGT leaves it alone", err)
	}
	return doc, nil
}

// writeConfig writes a configuration file, keeping a copy of the original
// from before UGT first changed it.
func writeConfig(file string, doc map[string]any) error {
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(file); err == nil {
		mode = info.Mode().Perm()
	}
	if original, err := os.ReadFile(file); err == nil {
		backup := file + ".before-ugt"
		if _, err := os.Stat(backup); errors.Is(err, fs.ErrNotExist) {
			if err := os.WriteFile(backup, original, 0o600); err != nil {
				return err
			}
		}
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp := file + ".ugt-tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), mode); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

func run(ctx context.Context, name string, args ...string) (int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}
