package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/ledger"
)

// memAgents are AI apps: those UGT is connected to, and the recipes' MCP
// servers added to them, as app/server.
type memAgents struct {
	connected map[string]bool
	added     []string
}

func (m *memAgents) Find(context.Context) ([]AgentHost, error) {
	var out []AgentHost
	for name := range m.connected {
		out = append(out, AgentHost{Name: name, Title: name, Connected: true})
	}
	return out, nil
}
func (m *memAgents) Connect(_ context.Context, name string, s MCPServer) (string, error) {
	if s.Name == "ugt" || s.Name == "" {
		m.connected[name] = true
	} else {
		m.added = append(m.added, name+"/"+s.Name)
	}
	return "connected", nil
}
func (m *memAgents) Disconnect(_ context.Context, name, server string) (string, error) {
	if server == "ugt" || server == "" {
		delete(m.connected, name)
	} else {
		m.added = slices.DeleteFunc(m.added, func(a string) bool { return a == name+"/"+server })
	}
	return "disconnected", nil
}

func TestUninstall(t *testing.T) {
	engine := &fakeEngine{config: jobConfig(), services: services(true), volumesOf: map[string][]string{"ugt-example-tool-markdown": {"ugt-example-tool-markdown_data"}}}
	a, _, home := runApp(t, engine)
	agents := &memAgents{connected: map[string]bool{}}
	secrets := memSecrets{}
	a.Agents, a.Secrets, a.Values = agents, secrets, memValues{}
	a.Catalogues = append(a.Catalogues, &fakeCatalogue{source: catalogue.Local, dirs: map[string]string{"example/service/dashboard": exampleService}})
	ctx := context.Background()

	if _, err := a.Run(ctx, RunOptions{Project: "example/tool", Inputs: []string{userFile(t, home, "r.docx")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Connect(ctx, []string{"claude-desktop"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetSetting(ctx, "example/service/dashboard", "PROVIDER_API_KEY", "sk-1"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"ledger.jsonl", "settings.json"} {
		os.MkdirAll(a.Layout.State, 0o755)
		os.WriteFile(filepath.Join(a.Layout.State, f), []byte("{}"), 0o644)
	}
	mine := filepath.Join(a.Layout.Recipes(), "me", "tool", "x")
	os.MkdirAll(mine, 0o755)

	preview, err := a.Uninstall(ctx, false)
	if err != nil || preview.Applied || len(preview.Steps) != 4 {
		t.Fatalf("preview %+v, %v", preview, err)
	}
	if !agents.connected["claude-desktop"] || len(secrets) != 1 {
		t.Fatal("the preview changed something")
	}

	res, err := a.Uninstall(ctx, true)
	if err != nil {
		t.Fatalf("uninstall %+v: %v", res, err)
	}
	if agents.connected["claude-desktop"] || len(secrets) != 0 {
		t.Errorf("left a connection or a secret: %v %v", agents.connected, secrets)
	}
	ran := strings.Join(engine.ran, "\n")
	if !strings.Contains(ran, "rmi ugt/example/tool/markdown:") || !strings.Contains(ran, "rmvol ugt-example-tool-markdown_data") {
		t.Errorf("images or data not removed:\n%s", ran)
	}
	if _, err := os.Stat(filepath.Join(a.Layout.State, "settings.json")); !os.IsNotExist(err) {
		t.Error("UGT's state stayed")
	}
	if _, err := os.Stat(mine); err != nil {
		t.Error("uninstall removed a recipe the user made")
	}
	if _, err := os.Stat(filepath.Join(home, "example", "tool", "markdown", "document.md")); err != nil {
		t.Error("uninstall removed the user's results")
	}
}

// Where UGT created the runtime, tearing it down removes everything in it.
func TestUninstallTearsDownWhatSetupCreated(t *testing.T) {
	a, led := newApp(t, &fakeEngine{})
	rt := &fakeRuntime{root: a.Runtime.Workspaces()}
	a.Runtime = rt
	led.Record(ledger.Entry{Kind: ledger.Runtime, Name: "wsl:UGT"})
	led.Record(ledger.Entry{Kind: ledger.Image, Name: "ugt/a/b/c:1", Recipe: "a/b/c"})
	res, err := a.Uninstall(context.Background(), true)
	if err != nil || !rt.tornDown || len(res.Steps) != 1 || !strings.Contains(res.Steps[0], "wsl:UGT") {
		t.Fatalf("uninstall %+v, %v, torn down %v", res, err, rt.tornDown)
	}
}
