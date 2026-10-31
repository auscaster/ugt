package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/auscaster/ugt/internal/domain/compose"
	"github.com/auscaster/ugt/internal/domain/ledger"
	"github.com/auscaster/ugt/internal/domain/recipe"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// Reachable is a page or an endpoint, at the address UGT published it on.
type Reachable struct {
	URL  string `json:"url"`
	What string `json:"what"`
}

// CommandReady is a command a recipe provides, on the user's PATH.
type CommandReady struct {
	Name string `json:"name"`
	What string `json:"what"`
	// Type is what the user types: the name, or `ugt command <name>` while
	// the launchers' folder is not on their PATH.
	Type string `json:"type"`
	Note string `json:"note,omitempty"`
}

// MCPReady is an MCP server a recipe provides, and the AI apps it was added to.
type MCPReady struct {
	Server string   `json:"server"` // its name in the apps' configuration
	What   string   `json:"what"`
	Apps   []string `json:"apps,omitempty"`
	Note   string   `json:"note,omitempty"`
}

// publishing picks the host port each address a recipe provides is
// published on: the one it had last time when that is still free, so the
// address stays the same.
func (a *App) publishing(p recipe.Provides, last map[string]int) ([]compose.Publish, map[string]int, error) {
	ports, used := map[string]int{}, map[int]bool{}
	var out []compose.Publish
	for _, ad := range p.Addresses() {
		key := portKey(ad)
		if _, done := ports[key]; done {
			continue
		}
		host, err := a.FreePort(last[key])
		for tries := 0; err == nil && used[host] && tries < 10; tries++ {
			host, err = a.FreePort(0)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("find a free port for services.%s port %d: %w", ad.Service, ad.Port, err)
		}
		ports[key], used[host] = host, true
		out = append(out, compose.Publish{Service: ad.Service, Target: ad.Port, Host: host})
	}
	return out, ports, nil
}

func portKey(ad recipe.Address) string { return fmt.Sprintf("%s:%d", ad.Service, ad.Port) }

// reachable says where each address is: http unless it names its protocol.
func reachable(list []recipe.Address, ports map[string]int) []Reachable {
	var out []Reachable
	for _, ad := range list {
		scheme, where := ad.Protocol, ad.Path
		if scheme == "" {
			scheme = "http"
		}
		if where == "" && (scheme == "http" || scheme == "https") {
			where = "/"
		}
		out = append(out, Reachable{URL: fmt.Sprintf("%s://127.0.0.1:%d%s", scheme, ports[portKey(ad)], where), What: ad.What})
	}
	return out
}

// provide sets up the commands and MCP servers a recipe provides, once a run
// has proved it works here: a launcher on the user's PATH for each command,
// and each MCP server added to every AI app UGT is connected to.
func (a *App) provide(ctx context.Context, f *recipe.Folder, res *RunResult) error {
	id := f.ID.String()
	for _, c := range f.Recipe.Provides.Commands {
		if a.Launchers == nil {
			return errors.New("this ugt cannot put commands on the PATH")
		}
		if _, err := a.Launchers.Install(c.Name); err != nil {
			return fmt.Errorf("put the command %s on the PATH: %w", c.Name, err)
		}
		_ = a.record(ledger.Entry{Kind: ledger.Command, Name: c.Name, Recipe: id})
		ready := CommandReady{Name: c.Name, What: c.What, Type: c.Name}
		if on, dir := a.Launchers.OnPath(); !on {
			ready.Type = "ugt command " + c.Name
			ready.Note = fmt.Sprintf("%s is not on your PATH: add it there to type %s alone", dir, c.Name)
		}
		res.Commands = append(res.Commands, ready)
	}
	servers := f.Recipe.Provides.MCPServers
	if len(servers) == 0 {
		return nil
	}
	found, err := a.Agents.Find(ctx)
	if err != nil {
		return err
	}
	for _, m := range servers {
		ready := MCPReady{Server: mcpName(f.ID, m.Service, len(servers)), What: m.What}
		server := MCPServer{Name: ready.Server, Command: a.Self.Command, Args: []string{"mcp", id, m.Service}}
		for _, h := range found {
			if !h.Connected {
				continue
			}
			if _, err := a.Agents.Connect(ctx, h.Name, server); err != nil {
				return fmt.Errorf("add %s to %s: %w", ready.Server, h.Title, err)
			}
			_ = a.record(ledger.Entry{Kind: ledger.MCPServer, Name: h.Name + "/" + ready.Server, Recipe: id})
			ready.Apps = append(ready.Apps, h.Title)
		}
		if len(ready.Apps) == 0 {
			ready.Note = "UGT is not connected to an AI app here: connect one with ugt connect, then run this again"
		} else {
			ready.Note = "restart " + strings.Join(ready.Apps, " and ") + " to load it"
		}
		res.MCPServers = append(res.MCPServers, ready)
	}
	return nil
}

// mcpName is a recipe's MCP server's name in an app's configuration.
func mcpName(id recipe.ID, service string, servers int) string {
	if servers > 1 {
		return id.Project() + "-" + service
	}
	return id.Project()
}

// RunCommand runs a command a recipe provides, as the user typed it: its
// service, with their arguments, in the folder to names, which the runtime
// reaches for the command's length. It returns the command's own exit code.
func (a *App) RunCommand(ctx context.Context, name string, args []string, to Attached) (int, error) {
	live, err := a.live()
	if err != nil {
		return 0, err
	}
	var id string
	for _, e := range live {
		if e.Kind == ledger.Command && e.Name == name {
			id = e.Recipe
		}
	}
	if id == "" {
		return 0, fmt.Errorf("no recipe UGT ran here provides the command %s", name)
	}
	r, p, err := a.provided(ctx, id)
	if err != nil {
		return 0, err
	}
	for _, c := range r.Provides.Commands {
		if c.Name != name {
			continue
		}
		if to.Folder != "" {
			folder, release, err := a.Runtime.Folder(ctx, to.Folder)
			if err != nil {
				return 0, err
			}
			defer release()
			to.Folder = folder
		}
		return a.Engine.Attach(ctx, p, c.Service, args, to)
	}
	return 0, fmt.Errorf("%s no longer provides the command %s: run ugt run %s again", id, name, id)
}

// ServeMCP runs an MCP server a recipe provides, speaking MCP over to's
// streams for the AI app that started it. service names the server when the
// recipe provides several.
func (a *App) ServeMCP(ctx context.Context, id, service string, to Attached) (int, error) {
	r, p, err := a.provided(ctx, id)
	if err != nil {
		return 0, err
	}
	for _, m := range r.Provides.MCPServers {
		if service == "" || m.Service == service {
			to.Folder, to.Terminal = "", false
			return a.Engine.Attach(ctx, p, m.Service, nil, to)
		}
	}
	return 0, fmt.Errorf("%s provides no MCP server %q", id, service)
}

// provided reads the recipe a run left in its workspace, and its project
// with the secrets its settings need.
func (a *App) provided(ctx context.Context, id string) (recipe.Recipe, Project, error) {
	parsed, err := recipe.ParseID(id)
	if err != nil {
		return recipe.Recipe{}, Project{}, err
	}
	files := a.Runtime.Files()
	ws := a.Layout.Workspace(parsed)
	_, p, err := readSession(files, ws)
	if err != nil {
		return recipe.Recipe{}, Project{}, fmt.Errorf("%s left nothing to run here (%v): run ugt run %s again", id, err, id)
	}
	data, err := files.ReadFile(path.Join(ws, workspace.RecipeDir, recipe.RecordFile))
	if err != nil {
		return recipe.Recipe{}, Project{}, err
	}
	var r recipe.Recipe
	if err := json.Unmarshal(data, &r); err != nil {
		return recipe.Recipe{}, Project{}, err
	}
	for _, s := range r.Settings {
		if s.Kind == "secret" && p.Env[s.Name] == "" && a.Secrets != nil {
			if v, ok, err := a.Secrets.Get(ctx, id, s.Name); err != nil {
				return r, p, err
			} else if ok {
				p.Env[s.Name] = v
			}
		}
	}
	return r, p, nil
}

// unprovide takes away a command's launcher or a recipe's MCP server in an
// AI app, by its ledger entry, and records that it is gone.
func (a *App) unprovide(ctx context.Context, e ledger.Entry) (string, error) {
	var done string
	switch e.Kind {
	case ledger.Command:
		if a.Launchers != nil {
			if err := a.Launchers.Remove(e.Name); err != nil {
				return "", err
			}
		}
		done = "removed the command " + e.Name
	case ledger.MCPServer:
		app, server, _ := strings.Cut(e.Name, "/")
		if _, err := a.Agents.Disconnect(ctx, app, server); err != nil {
			return "", err
		}
		done = "removed the MCP server " + server + " from " + app
	default:
		return "", nil
	}
	_ = a.record(ledger.Entry{Kind: e.Kind, Name: e.Name, Recipe: e.Recipe, Removed: a.Now()})
	return done, nil
}
