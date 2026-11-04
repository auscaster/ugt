// Package agents implements the AgentHosts port: it finds the AI apps on this
// machine and connects UGT's MCP server to each through that app's own
// documented mechanism. Claude Desktop, Cursor and VS Code read a JSON file;
// Claude Code and Codex have a command for it. A file is backed up once
// before UGT first changes it, and nothing else in it is touched.
//
// An app may have UGT already, from UGT's plugin (Claude Code, Codex) or its
// extension (Claude Desktop), installed in the app itself. That connects it:
// UGT adds no second server or skill there, removes the ones it added before
// the plugin came, and leaves the plugin to the app.
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
	// Output runs a command and returns what it printed: the apps' own
	// lists of their plugins and servers.
	Output func(ctx context.Context, name string, args ...string) ([]byte, error)
	// Skill is UGT's agent skill, its files at the root. Apps that read
	// skills get it installed as ugt/ beside the MCP server.
	Skill fs.FS
}

var _ app.AgentHosts = (*Hosts)(nil)

// New returns the hosts on this machine.
func New(goos, home string) *Hosts {
	return &Hosts{GOOS: goos, Home: home, Getenv: os.Getenv, LookPath: exec.LookPath, Run: run, Output: output}
}

// host is one AI app and how it is connected.
type host struct {
	name, title string
	file        string // its MCP configuration file, for apps configured by file
	key         string // the object in that file that holds servers
	vscode      bool   // VS Code's entries name their transport
	command     string // its command line, for apps configured by command
	skills      string // its skills folder, for apps that read skills
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
	codexHome := h.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(h.Home, ".codex")
	}
	return []host{
		{name: "claude-desktop", title: "Claude Desktop", file: filepath.Join(desktop, "claude_desktop_config.json"), key: "mcpServers"},
		{name: "claude-code", title: "Claude Code", command: "claude", skills: filepath.Join(h.Home, ".claude", "skills")},
		{name: "codex", title: "Codex", command: "codex", skills: filepath.Join(codexHome, "skills")},
		{name: "cursor", title: "Cursor", file: filepath.Join(h.Home, ".cursor", "mcp.json"), key: "mcpServers"},
		{name: "vscode", title: "VS Code", file: filepath.Join(vscode, "mcp.json"), key: "servers", vscode: true},
	}
}

// Find lists the AI apps installed here and whether UGT is connected to each.
func (h *Hosts) Find(ctx context.Context) ([]app.AgentHost, error) {
	var out []app.AgentHost
	for _, x := range h.hosts() {
		installed, l := h.state(ctx, x)
		if installed {
			where := x.file
			if where == "" {
				where = "the " + x.command + " command"
			}
			starts := l.plugin && h.starts(x)
			if starts && !l.own {
				where = "UGT's " + x.pluginKind() + " in " + x.title
			}
			// twice: UGT's own entry beside an extension that runs its own ugt, or
			// UGT's own copy of the skill beside the plugin's
			twice := (l.own && starts) || (l.plugin && !starts && h.ownSkill(x))
			out = append(out, app.AgentHost{Name: x.name, Title: x.title, Connected: l.own || starts, Where: where, Twice: twice})
		}
	}
	return out, nil
}

// link is how UGT reaches one app: its own entry, which ugt connect adds, and
// UGT's plugin or extension, which the user installed in the app.
type link struct{ own, plugin bool }

func (h *Hosts) state(ctx context.Context, x host) (installed bool, l link) {
	if x.command != "" {
		if _, err := h.LookPath(x.command); err != nil {
			return false, link{}
		}
		return true, h.commandLink(ctx, x)
	}
	if _, err := os.Stat(filepath.Dir(x.file)); err != nil {
		return false, link{}
	}
	l.plugin = x.name == "claude-desktop" && extension(filepath.Dir(x.file))
	doc, err := readConfig(x.file)
	if err != nil {
		return true, l // a file UGT cannot read holds no entry of UGT's
	}
	servers, _ := doc[x.key].(map[string]any)
	_, l.own = servers[Name]
	return true, l
}

// commandLink asks an app with a command which way UGT reaches it. Claude
// Code names a plugin's server plugin:<plugin>:<server>, so its own entry is
// the one named ugt; Codex names a plugin's server as the plugin does and an
// entry of its own hides it, so the plugin is read from its list of plugins.
// An app too old to list its plugins has none of UGT's.
func (h *Hosts) commandLink(ctx context.Context, x host) (l link) {
	exit, err := h.Run(ctx, x.command, "mcp", "get", Name)
	l.own = err == nil && exit == 0
	if h.Output == nil {
		return l
	}
	switch x.command {
	case "claude":
		out, err := h.Output(ctx, "claude", "plugin", "list", "--json")
		var plugins []struct {
			ID         string `json:"id"`
			Enabled    bool   `json:"enabled"`
			MCPServers map[string]struct {
				Command string
				Args    []string
			} `json:"mcpServers"`
		}
		if err == nil && json.Unmarshal(out, &plugins) == nil {
			for _, p := range plugins {
				l.plugin = l.plugin || (p.Enabled && strings.HasPrefix(p.ID, Name+"@"))
				for _, s := range p.MCPServers {
					l.plugin = l.plugin || (p.Enabled && runsUGT(s.Command, s.Args))
				}
			}
		}
	case "codex":
		if out, err := h.Output(ctx, "codex", "mcp", "list", "--json"); err == nil {
			var servers []struct {
				Name      string `json:"name"`
				Transport struct {
					Env map[string]string `json:"env"`
				} `json:"transport"`
			}
			if json.Unmarshal(out, &servers) == nil {
				l.own = false
				for _, s := range servers {
					l.own = l.own || (s.Name == Name && s.Transport.Env["PLUGIN_ROOT"] == "")
				}
			}
		}
		out, err := h.Output(ctx, "codex", "plugin", "list", "--json")
		var plugins struct {
			Installed []struct {
				Name    string `json:"name"`
				Enabled bool   `json:"enabled"`
			} `json:"installed"`
		}
		if err == nil && json.Unmarshal(out, &plugins) == nil {
			for _, p := range plugins.Installed {
				l.plugin = l.plugin || (p.Enabled && p.Name == Name)
			}
		}
	}
	return l
}

// starts says whether UGT's plugin or extension in an app starts UGT on its
// own, so the app can be left to it. Claude Desktop's extension carries its own
// ugt. A plugin starts ugt from the PATH its app was started with, which ugt
// cannot see, so an app with UGT's plugin keeps UGT's own entry, which runs ugt
// by its full path; Codex hides its plugin's server behind that entry, and
// Claude Code's plugin brings only the skill.
func (h *Hosts) starts(x host) bool { return x.name == "claude-desktop" }

// ownSkill says whether UGT put its own copy of the skill in the app.
func (h *Hosts) ownSkill(x host) bool {
	if x.skills == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(x.skills, Name))
	return err == nil
}

// runsUGT says whether a server's command is ugt's MCP server.
func runsUGT(command string, args []string) bool {
	base := strings.TrimSuffix(filepath.Base(command), ".exe")
	return base == Name && len(args) > 0 && args[0] == "mcp"
}

func (x host) pluginKind() string {
	if x.name == "claude-desktop" {
		return "extension"
	}
	return "plugin"
}

// extension says whether UGT's extension is installed and enabled in Claude
// Desktop, whose folder is dir. Claude Desktop unpacks each extension into its
// own folder in Claude Extensions there, with the bundle's manifest.json,
// which names UGT's ugt, and keeps whether it is enabled in Claude Extensions
// Settings, by the same name: it writes isEnabled on installing one and starts
// only those that have it. A disabled extension connects nothing.
func extension(dir string) bool {
	manifests, _ := filepath.Glob(filepath.Join(dir, "Claude Extensions", "*", "manifest.json"))
	for _, file := range manifests {
		var m struct{ Name string }
		var settings struct {
			IsEnabled bool `json:"isEnabled"`
		}
		id := filepath.Base(filepath.Dir(file))
		if readJSON(file, &m) && m.Name == Name &&
			readJSON(filepath.Join(dir, "Claude Extensions Settings", id+".json"), &settings) && settings.IsEnabled {
			return true
		}
	}
	return false
}

func readJSON(file string, v any) bool {
	data, err := os.ReadFile(file)
	return err == nil && json.Unmarshal(data, v) == nil
}

// Connect adds UGT's server to an app and says what changed. Restarting the
// app makes it load the server.
func (h *Hosts) Connect(ctx context.Context, name string, s app.MCPServer) (string, error) {
	x, err := h.find(name)
	if err != nil {
		return "", err
	}
	_, l := h.state(ctx, x)
	if l.plugin && h.starts(x) {
		return h.leaveToPlugin(ctx, x, l)
	}
	kept := ""
	if l.plugin {
		kept = "; UGT's " + x.pluginKind() + " in " + x.title + " brings the skill, and this server, which runs ugt by its full path, starts it whatever the app's PATH"
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
		done := fmt.Sprintf("added the %s MCP server with %s mcp add", Name, x.command)
		if l.plugin && x.skills != "" {
			// the plugin brings the skill, so UGT's own copy goes
			if err := os.RemoveAll(filepath.Join(x.skills, Name)); err != nil {
				return "", err
			}
		} else if x.skills != "" && h.Skill != nil {
			dir := filepath.Join(x.skills, Name)
			if err := os.RemoveAll(dir); err != nil {
				return "", err
			}
			if err := os.CopyFS(dir, h.Skill); err != nil {
				return "", fmt.Errorf("install the %s skill in %s: %w", Name, dir, err)
			}
			done += " and the " + Name + " skill to " + dir
		}
		return done + kept, nil
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
	_, l := h.state(ctx, x)
	if l.plugin && x.name == "claude-code" {
		// Claude Code's plugin brings only the skill
		if l.own {
			if err := h.removeOwn(ctx, x); err != nil {
				return "", err
			}
		}
		return "removed the " + Name + " MCP server from " + x.title + "; UGT's plugin there brings UGT's skill: remove the plugin to remove that too", nil
	}
	if l.plugin {
		done, err := h.leaveToPlugin(ctx, x, l)
		if err != nil {
			return "", err
		}
		return done + "; to disconnect it, remove UGT's " + x.pluginKind() + " in " + x.title, nil
	}
	if !l.own {
		if x.skills != "" {
			if err := os.RemoveAll(filepath.Join(x.skills, Name)); err != nil {
				return "", err
			}
		}
		return x.title + " was not connected", nil
	}
	if x.command != "" {
		if err := h.removeOwn(ctx, x); err != nil {
			return "", err
		}
		return "removed the " + Name + " MCP server and skill from " + x.title, nil
	}
	if err := h.removeEntry(x); err != nil {
		return "", err
	}
	return "removed the " + Name + " MCP server from " + x.file, nil
}

// leaveToPlugin leaves an app to UGT's plugin or extension in it, removing
// the entry and skill UGT added there before it came.
func (h *Hosts) leaveToPlugin(ctx context.Context, x host, l link) (string, error) {
	done := x.title + " has UGT's " + x.pluginKind() + ", which connects it"
	if !l.own {
		if x.skills != "" {
			if err := os.RemoveAll(filepath.Join(x.skills, Name)); err != nil {
				return "", err
			}
		}
		return done, nil
	}
	if x.command != "" {
		if err := h.removeOwn(ctx, x); err != nil {
			return "", err
		}
	} else if err := h.removeEntry(x); err != nil {
		return "", err
	}
	return done + "; removed the " + Name + " MCP server UGT had added beside it", nil
}

// removeOwn removes UGT's own entry and skill from an app with a command.
func (h *Hosts) removeOwn(ctx context.Context, x host) error {
	args := []string{"mcp", "remove", Name}
	if x.command == "claude" {
		args = append(args, "--scope", "user")
	}
	if exit, err := h.Run(ctx, x.command, args...); err != nil || exit != 0 {
		return fmt.Errorf("%s %s failed (exit %d): %v", x.command, strings.Join(args, " "), exit, err)
	}
	if x.skills != "" {
		return os.RemoveAll(filepath.Join(x.skills, Name))
	}
	return nil
}

// removeEntry removes UGT's own entry from an app's configuration file.
func (h *Hosts) removeEntry(x host) error {
	doc, err := readConfig(x.file)
	if err != nil {
		return fmt.Errorf("%s: %w", x.file, err)
	}
	servers, _ := doc[x.key].(map[string]any)
	delete(servers, Name)
	return writeConfig(x.file, doc)
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

func output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
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
