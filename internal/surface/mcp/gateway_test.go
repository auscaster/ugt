package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/usegeekthing/ugt/internal/app"
)

// recipeServers are the MCP servers recipes provide: each serves one tool,
// "now", that answers with its recipe's name. other's are older servers.
type recipeServers struct {
	mu       sync.Mutex
	provided []app.ProvidedMCP
	saved    map[string]string
	started  []string
}

func (r *recipeServers) MCPServers() ([]app.ProvidedMCP, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.provided), nil
}

func (r *recipeServers) SaveMCPTools(id, service string, tools []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saved[id+"/"+service] = string(tools)
	return nil
}

func (r *recipeServers) ServeMCP(ctx context.Context, id, service string, to app.Attached) (int, error) {
	r.mu.Lock()
	r.started = append(r.started, id+"/"+service)
	r.mu.Unlock()
	if strings.HasPrefix(id, "other/") {
		legacyServer(id, to.Stdin, to.Stdout.(io.WriteCloser))
		return 0, nil
	}
	srv := sdk.NewServer(&sdk.Implementation{Name: id}, nil)
	sdk.AddTool(srv, &sdk.Tool{Name: "now", Description: "Says the time."}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "it is now, says " + id}}}, nil, nil
	})
	err := srv.Run(ctx, &sdk.IOTransport{Reader: to.Stdin.(io.ReadCloser), Writer: to.Stdout.(io.WriteCloser)})
	return 0, err
}

func (r *recipeServers) provide(p app.ProvidedMCP) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.provided = append(r.provided, p)
}

// An AI app connected to UGT gets the tools of the MCP servers recipes
// provide among UGT's own, at once and named after their recipe; a call
// starts the server once and relays to it; a server removed takes its tools
// with it.
func TestRecipesMCPServersThroughUGT(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cached := `[{"name":"now","description":"Says the time.","inputSchema":{"type":"object"}}]`
	recipes := &recipeServers{saved: map[string]string{}, provided: []app.ProvidedMCP{{Recipe: "acme/clock/time", Service: "mcp", Tools: []byte(cached)}}}

	srv := sdk.NewServer(&sdk.Implementation{Name: "ugt"}, nil)
	gw := newGateway(ctx, recipes, srv, "test", nil)
	t.Cleanup(gw.close)
	gw.refresh(false)
	serverT, clientT := sdk.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, serverT) }()
	changed := make(chan struct{}, 4)
	client := sdk.NewClient(&sdk.Implementation{Name: "an AI app"}, &sdk.ClientOptions{
		ToolListChangedHandler: func(context.Context, *sdk.ToolListChangedRequest) { changed <- struct{}{} },
	})
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := func() []string {
		t.Helper()
		res, err := session.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tool := range res.Tools {
			names = append(names, tool.Name)
			if !strings.Contains(tool.Description, "run by UGT") {
				t.Errorf("%s does not say where it comes from: %q", tool.Name, tool.Description)
			}
		}
		slices.Sort(names)
		return names
	}
	// the kept tools are shown with nothing started
	if got := tools(); !slices.Equal(got, []string{"time_now"}) || len(recipes.started) != 0 {
		t.Fatalf("tools %v, started %v", got, recipes.started)
	}

	res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "time_now", Arguments: map[string]any{}})
	if err != nil || res.IsError || res.Content[0].(*sdk.TextContent).Text != "it is now, says acme/clock/time" {
		t.Fatalf("call %+v, %v", res, err)
	}
	if _, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "time_now", Arguments: map[string]any{}}); err != nil || len(recipes.started) != 1 {
		t.Fatalf("a second call started it again: %v, %v", recipes.started, err)
	}

	// a run provides another with the same task: it is started to list its
	// tools, which are kept, and both are named by repository and task
	recipes.provide(app.ProvidedMCP{Recipe: "other/watch/time", Service: "mcp"})
	gw.refresh(true)
	waitFor(t, changed)
	if got := tools(); !slices.Equal(got, []string{"clock_time_now", "watch_time_now"}) {
		t.Fatalf("tools %v", got)
	}
	var kept []map[string]any
	if err := json.Unmarshal([]byte(recipes.saved["other/watch/time/mcp"]), &kept); err != nil || len(kept) != 1 || kept[0]["name"] != "now" {
		t.Errorf("kept %q", recipes.saved)
	}

	recipes.mu.Lock()
	recipes.provided = recipes.provided[1:]
	recipes.mu.Unlock()
	gw.refresh(true)
	waitFor(t, changed)
	if got := tools(); !slices.Equal(got, []string{"time_now"}) {
		t.Fatalf("after removal %v", got)
	}
	if res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "time_now", Arguments: map[string]any{}}); err != nil || res.Content[0].(*sdk.TextContent).Text != "it is now, says other/watch/time" {
		t.Fatalf("call after removal %+v, %v", res, err)
	}
}

// waitFor waits until the AI app is told the tools changed.
func waitFor(t *testing.T, changed chan struct{}) {
	t.Helper()
	<-changed
}

// legacyServer answers as many stdio MCP servers do: the initialize
// handshake and its tools, and nothing to a request it does not know.
func legacyServer(id string, in io.Reader, out io.WriteCloser) {
	defer out.Close()
	lines := bufio.NewScanner(in)
	enc := json.NewEncoder(out)
	for lines.Scan() {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(lines.Bytes(), &m)
		var result any
		switch m.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": id, "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "now", "description": "Says the time.", "inputSchema": map[string]any{"type": "object"}}}}
		case "ping":
			result = map[string]any{}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "it is now, says " + id}}}
		}
		if result != nil && m.ID != nil {
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		}
	}
}

// An exited child may leave its borrowed stdin copier blocked until the next
// write. Model that delayed EOF: do not let a mutating tool discover it, and
// never replay a tool whose result was lost after it changed state.
func TestGatewayReconnectsBeforeCallsWithoutReplayingWrites(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	recipes := &interruptedServers{recipeServers: recipeServers{saved: map[string]string{}, provided: []app.ProvidedMCP{{Recipe: "acme/clock/time", Service: "mcp", Tools: []byte(`[{"name":"now","inputSchema":{"type":"object"}}]`)}}}}
	srv := sdk.NewServer(&sdk.Implementation{Name: "ugt"}, nil)
	gw := newGateway(ctx, recipes, srv, "test", nil)
	defer gw.close()
	gw.refresh(false)
	serverT, clientT := sdk.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, serverT) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "client"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(wantError bool) {
		t.Helper()
		res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "time_now", Arguments: map[string]any{}})
		if err != nil || res.IsError != wantError {
			t.Fatalf("call: %+v, %v", res, err)
		}
	}
	call(false)
	recipes.mu.Lock()
	recipes.stopped = true
	recipes.mu.Unlock()
	call(false) // the first call after replacement succeeds on a fresh backend
	recipes.mu.Lock()
	if len(recipes.started) != 2 || recipes.calls != 2 {
		t.Errorf("after replacement: starts %v, writes %d", recipes.started, recipes.calls)
	}
	recipes.loseResult = true
	recipes.mu.Unlock()
	call(true) // the write happened, but its result was lost: report, never retry
	recipes.mu.Lock()
	if recipes.calls != 3 {
		t.Errorf("the ambiguous write ran %d times", recipes.calls-2)
	}
	recipes.mu.Unlock()
	call(false) // a later explicit request can start a fresh server
	recipes.mu.Lock()
	defer recipes.mu.Unlock()
	if len(recipes.started) != 3 || recipes.calls != 4 {
		t.Errorf("after recovery: starts %v, writes %d", recipes.started, recipes.calls)
	}
}

type interruptedServers struct {
	recipeServers
	stopped, loseResult bool
	calls               int
}

func (r *interruptedServers) ServeMCP(_ context.Context, id, service string, to app.Attached) (int, error) {
	r.mu.Lock()
	r.started = append(r.started, id+"/"+service)
	r.mu.Unlock()
	lines, enc := bufio.NewScanner(to.Stdin), json.NewEncoder(to.Stdout)
	for lines.Scan() {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(lines.Bytes(), &m)
		r.mu.Lock()
		stopped := r.stopped
		r.stopped = false
		if m.Method == "tools/call" {
			r.calls++
			stopped = stopped || r.loseResult
			r.loseResult = false
		}
		r.mu.Unlock()
		if stopped {
			return 0, nil
		}
		var result any
		switch m.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": protocol, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": id, "version": "1"}}
		case "ping":
			result = map[string]any{}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "written"}}}
		}
		if result != nil {
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		}
	}
	return 0, nil
}

func TestToolPrefixes(t *testing.T) {
	got := toolPrefixes([]app.ProvidedMCP{
		{Recipe: "a/clock/time", Service: "mcp"},
		{Recipe: "b/clock/time", Service: "mcp"},
		{Recipe: "c/tools/ugt", Service: "mcp"},
		{Recipe: "d/suite/notes", Service: "read"},
		{Recipe: "d/suite/notes", Service: "write"},
		{Recipe: "e/my.repo/files", Service: "mcp"},
	})
	want := map[string]string{
		"a/clock/time/mcp":    "a_clock_time",
		"b/clock/time/mcp":    "b_clock_time",
		"c/tools/ugt/mcp":     "tools_ugt",
		"d/suite/notes/read":  "notes_read",
		"d/suite/notes/write": "notes_write",
		"e/my.repo/files/mcp": "files",
	}
	for key, prefix := range want {
		if got[key] != prefix {
			t.Errorf("%s is %q, want %q", key, got[key], prefix)
		}
	}
	if name := toolName("files", strings.Repeat("x", 80)); len(name) != maxTool || !strings.HasPrefix(name, "files_xxx") {
		t.Errorf("long name %q", name)
	}
}
