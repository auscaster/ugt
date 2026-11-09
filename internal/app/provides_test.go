package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/usegeekthing/ugt/internal/domain/catalogue"
	"github.com/usegeekthing/ugt/internal/domain/compose"
)

const exampleCommand = "../../spec/recipe/v0/examples/valid/example-command"

// A job that provides a command and an MCP server proves itself on its
// sample, then puts the command on the PATH and serves the MCP server among
// UGT's own tools, keeping what it lists; using them runs their services, and
// removing the recipe takes them away.
func TestProvidedCommandsAndMCPServers(t *testing.T) {
	ok := func(string, io.Writer) int { return 0 }
	engine := &fakeEngine{
		config: compose.Model{Services: map[string]compose.Service{
			"task": offline(), "check": offline("check"), "sample": offline("sample"), "sample-check": offline("sample"),
			"cli": offline("command"), "mcp": offline("mcp"),
		}},
		services: map[string]func(string, io.Writer) int{"task": ok, "check": ok, "sample": ok, "sample-check": ok},
	}
	a, _ := newApp(t, engine)
	launchers := memLaunchers{}
	a.Launchers, a.Agents = launchers, &memAgents{connected: map[string]bool{"claude-code": true}}
	a.Catalogues = []Catalogue{&fakeCatalogue{source: catalogue.Local, dirs: map[string]string{"example/tool/cli": exampleCommand}}}
	ctx := context.Background()

	res, err := a.Run(ctx, RunOptions{Project: "example/tool"})
	if err != nil || !res.Passed {
		t.Fatalf("run %+v, %v", res, err)
	}
	if got := strings.Join(engine.ran, ","); got != "sample,task,check,sample-check" {
		t.Errorf("ran %s", got)
	}
	if len(res.Commands) != 1 || res.Commands[0].Type != "tool" || !launchers["tool"] {
		t.Errorf("commands %+v, launchers %v", res.Commands, launchers)
	}
	if len(res.MCPServers) != 1 || res.MCPServers[0].Service != "mcp" || !strings.Contains(res.MCPServers[0].Note, "among UGT's own") {
		t.Errorf("MCP servers %+v", res.MCPServers)
	}
	servers, err := a.MCPServers()
	if err != nil || len(servers) != 1 || servers[0].Recipe != "example/tool/cli" || servers[0].Service != "mcp" || servers[0].Tools != nil {
		t.Fatalf("served %+v, %v", servers, err)
	}
	if err := a.SaveMCPTools("example/tool/cli", "mcp", []byte(`[{"name":"echo"}]`)); err != nil {
		t.Fatal(err)
	}
	if servers, _ := a.MCPServers(); len(servers) != 1 || string(servers[0].Tools) != `[{"name":"echo"}]` {
		t.Errorf("the tools it listed were not kept: %+v", servers)
	}

	if _, err := a.RunCommand(ctx, "tool", []string{"--version"}, Attached{Folder: "/Users/me/docs"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ServeMCP(ctx, "example/tool/cli", "mcp", Attached{}); err != nil {
		t.Fatal(err)
	}
	// the command reaches the folder through the runtime, for its length only
	if got := engine.ran[len(engine.ran)-2:]; !slices.Equal(got, []string{"attach cli --version in /mnt/ugt/Users/me/docs", "attach mcp"}) {
		t.Errorf("used as %q", got)
	}
	if released := a.Runtime.(*fakeRuntime).released; !slices.Equal(released, []string{"/mnt/ugt/Users/me/docs"}) {
		t.Errorf("the folder was let go as %q", released)
	}

	if _, err := a.Remove(ctx, "example/tool/cli"); err != nil {
		t.Fatal(err)
	}
	if servers, _ := a.MCPServers(); launchers["tool"] || len(servers) != 0 {
		t.Errorf("remove left the command %v or the MCP server %v", launchers, servers)
	}
	if _, err := os.Stat(filepath.Join(a.Layout.State, "mcp", "example", "tool", "cli", "mcp.json")); !os.IsNotExist(err) {
		t.Errorf("remove left the tools it listed: %v", err)
	}
	if _, err := a.RunCommand(ctx, "tool", nil, Attached{}); err == nil {
		t.Error("a removed command still ran")
	}
}

func offline(profiles ...string) compose.Service {
	return compose.Service{Profiles: profiles, NetworkMode: "none"}
}
