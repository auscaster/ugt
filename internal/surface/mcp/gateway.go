package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/usegeekthing/ugt/internal/app"
	"github.com/usegeekthing/ugt/internal/domain/recipe"
)

// gateway serves the MCP servers recipes provide through UGT's own: their
// tools join UGT's, named after their recipe's task, so an AI app connected
// to UGT has them as soon as a run provides them, with no restart and no
// change to its configuration. A server starts the first time one of its
// tools is called and runs until UGT's server stops. The tools it lists are
// kept, so an AI app sees them before it starts again.
type gateway struct {
	app     servers
	srv     *sdk.Server
	version string
	log     io.Writer       // why a server could not be served, for the AI app's log
	ctx     context.Context // backends run under the server's lifetime

	refreshing sync.Mutex
	mu         sync.Mutex
	mounted    map[string][]byte // tool name to its definition, as served
	backends   map[string]*backend
}

// servers are the MCP servers recipes provide, as the App holds them.
type servers interface {
	MCPServers() ([]app.ProvidedMCP, error)
	SaveMCPTools(recipeID, service string, tools []byte) error
	ServeMCP(ctx context.Context, id, service string, to app.Attached) (int, error)
}

// backend is one recipe's MCP server, started for UGT's server. ready
// closes once it answered, or failed to.
type backend struct {
	ready   chan struct{}
	session *sdk.ClientSession
	err     error
	stderr  tailBuffer
}

// handshake bounds how long a server may take to start and answer.
const handshake = time.Minute

// protocol is the MCP version UGT asks a server for: one whose handshake every
// stdio server answers; the server may agree to an older one.
const protocol = "2025-11-25"

func newGateway(ctx context.Context, a servers, srv *sdk.Server, version string, log io.Writer) *gateway {
	if log == nil {
		log = io.Discard
	}
	return &gateway{app: a, srv: srv, version: version, log: log, ctx: ctx, mounted: map[string][]byte{}, backends: map[string]*backend{}}
}

// refresh serves the tools of the MCP servers provided now, and no others.
// With start, a server whose tools are not known yet is started to list
// them; without, it waits for a refresh that may.
func (g *gateway) refresh(start bool) {
	g.refreshing.Lock()
	defer g.refreshing.Unlock()
	provided, err := g.app.MCPServers()
	if err != nil {
		return
	}
	prefixes := toolPrefixes(provided)
	want := map[string][]byte{}
	handlers := map[string]sdk.ToolHandler{}
	for _, p := range provided {
		key := p.Recipe + "/" + p.Service
		tools := p.Tools
		if tools == nil && !start {
			continue
		}
		if tools == nil {
			if tools, err = g.list(key, p); err != nil {
				fmt.Fprintf(g.log, "ugt: %v\n", err)
				continue
			}
		}
		var listed []*sdk.Tool
		if err := json.Unmarshal(tools, &listed); err != nil {
			fmt.Fprintf(g.log, "ugt: the tools %s's MCP server %s listed: %v\n", p.Recipe, p.Service, err)
			continue
		}
		for _, t := range listed {
			name := toolName(prefixes[key], t.Name)
			if !servable(t) {
				fmt.Fprintf(g.log, "ugt: %s's MCP server %s: the tool %q takes no object, so AI apps cannot call it\n", p.Recipe, p.Service, t.Name)
				continue
			}
			if want[name] != nil {
				continue
			}
			served := *t
			served.Name = name
			served.Description = strings.TrimSpace(t.Description + " (from " + p.Recipe + ", run by UGT)")
			def, _ := json.Marshal(&served)
			want[name], handlers[name] = def, g.call(key, p, t.Name)
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var gone []string
	for name := range g.mounted {
		if want[name] == nil {
			gone = append(gone, name)
		}
	}
	if len(gone) > 0 {
		g.srv.RemoveTools(gone...)
	}
	for name, def := range want {
		if string(g.mounted[name]) == string(def) {
			continue
		}
		var t sdk.Tool
		_ = json.Unmarshal(def, &t)
		g.srv.AddTool(&t, handlers[name])
	}
	g.mounted = want
	for key, b := range g.backends {
		if !slices.ContainsFunc(provided, func(p app.ProvidedMCP) bool { return p.Recipe+"/"+p.Service == key }) {
			b.stop()
			delete(g.backends, key)
		}
	}
}

// list starts a server to learn its tools, and keeps them.
func (g *gateway) list(key string, p app.ProvidedMCP) ([]byte, error) {
	b, err := g.backend(key, p)
	if err != nil {
		return nil, err
	}
	res, err := b.session.ListTools(g.ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list the tools of %s's MCP server %s: %w", p.Recipe, p.Service, err)
	}
	tools, err := json.Marshal(res.Tools)
	if err != nil {
		return nil, err
	}
	_ = g.app.SaveMCPTools(p.Recipe, p.Service, tools)
	return tools, nil
}

// call relays a call to one of a server's tools, starting the server when it
// is not running.
func (g *gateway) call(key string, p app.ProvidedMCP, tool string) sdk.ToolHandler {
	return func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		b, err := g.backend(key, p)
		if err != nil {
			return failed(err), nil
		}
		res, err := b.session.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: req.Params.Arguments})
		if err != nil {
			g.drop(key, b)
			return failed(fmt.Errorf("%s's MCP server %s failed: %w %s", p.Recipe, p.Service, err, b.stderr.String())), nil
		}
		return res, nil
	}
}

// backend returns the running server, starting it when it is not. Calls to
// one server wait while it starts; calls to the others do not.
func (g *gateway) backend(key string, p app.ProvidedMCP) (*backend, error) {
	g.mu.Lock()
	b, starting := g.backends[key], false
	if b == nil {
		b, starting = &backend{ready: make(chan struct{})}, true
		g.backends[key] = b
	}
	g.mu.Unlock()
	if starting {
		b.session, b.err = g.start(p, b)
		if b.err != nil {
			g.drop(key, b)
		} else {
			go func() {
				_ = b.session.Wait()
				g.drop(key, b)
			}()
		}
		close(b.ready)
	}
	<-b.ready
	return b, b.err
}

// start runs a server and makes the MCP handshake with it. A server that does
// not answer within the handshake's time has its input closed, which ends it.
func (g *gateway) start(p app.ProvidedMCP, b *backend) (*sdk.ClientSession, error) {
	toServer, input := io.Pipe()
	output, fromServer := io.Pipe()
	go func() {
		_, err := g.app.ServeMCP(g.ctx, p.Recipe, p.Service, app.Attached{Stdin: toServer, Stdout: fromServer, Stderr: &b.stderr})
		if err == nil {
			err = io.EOF
		}
		_ = fromServer.CloseWithError(err)
	}()
	type connected struct {
		session *sdk.ClientSession
		err     error
	}
	done := make(chan connected, 1)
	go func() {
		client := sdk.NewClient(&sdk.Implementation{Name: "ugt", Version: g.version}, nil)
		session, err := client.Connect(g.ctx, &sdk.IOTransport{Reader: output, Writer: input}, &sdk.ClientSessionOptions{ProtocolVersion: protocol})
		done <- connected{session, err}
	}()
	var c connected
	select {
	case c = <-done:
	case <-time.After(handshake):
		_, _ = input.Close(), output.Close()
		c = <-done
		c.err = fmt.Errorf("it did not answer within %s", handshake)
	}
	if c.err != nil {
		_ = input.Close()
		return nil, fmt.Errorf("start %s's MCP server %s: %w %s", p.Recipe, p.Service, c.err, strings.TrimSpace(b.stderr.String()))
	}
	return c.session, nil
}

// stop ends a server that started.
func (b *backend) stop() {
	select {
	case <-b.ready:
		if b.session != nil {
			_ = b.session.Close()
		}
	default:
	}
}

// drop forgets a server that stopped, so the next call starts it again.
func (g *gateway) drop(key string, b *backend) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.backends[key] == b {
		delete(g.backends, key)
	}
}

// close stops every server: closing its input ends a stdio MCP server, and
// its container with it.
func (g *gateway) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for key, b := range g.backends {
		b.stop()
		delete(g.backends, key)
	}
}

func failed(err error) *sdk.CallToolResult {
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: err.Error()}}}
}

// servable reports whether a server's tool can be served: AI apps require an
// object for a tool's input.
func servable(t *sdk.Tool) bool {
	var schema struct {
		Type string `json:"type"`
	}
	data, err := json.Marshal(t.InputSchema)
	return err == nil && json.Unmarshal(data, &schema) == nil && schema.Type == "object" && t.Name != ""
}

// toolPrefixes names each server's tools after its recipe's task, or its
// repository and task, or all of its ID, whichever is the shortest no other
// server, nor UGT's own tools, also takes; and its service too, when its
// recipe provides several.
func toolPrefixes(servers []app.ProvidedMCP) map[string]string {
	perRecipe := map[string]int{}
	for _, p := range servers {
		perRecipe[p.Recipe]++
	}
	candidates := map[string][]string{}
	level := map[string]int{}
	for _, p := range servers {
		key := p.Recipe + "/" + p.Service
		id, err := recipe.ParseID(p.Recipe)
		if err != nil {
			id = recipe.ID{Task: p.Recipe}
		}
		forms := []string{id.Task, id.Repo + "_" + id.Task, id.Owner + "_" + id.Repo + "_" + id.Task}
		for i := range forms {
			if perRecipe[p.Recipe] > 1 {
				forms[i] += "_" + p.Service
			}
			forms[i] = toolSafe(forms[i])
		}
		candidates[key] = forms
	}
	prefixes := map[string]string{}
	for {
		taken := map[string][]string{"ugt": {""}}
		for key, forms := range candidates {
			prefixes[key] = forms[level[key]]
			taken[prefixes[key]] = append(taken[prefixes[key]], key)
		}
		clash := false
		for _, keys := range taken {
			if len(keys) < 2 {
				continue
			}
			for _, key := range keys {
				if key != "" && level[key] < 2 {
					level[key]++
					clash = true
				}
			}
		}
		if !clash {
			return prefixes
		}
	}
}

var unsafeTool = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func toolSafe(s string) string { return strings.Trim(unsafeTool.ReplaceAllString(s, "_"), "_") }

// maxTool keeps a tool's name within what AI apps accept once they prefix it
// with the server's name.
const maxTool = 54

// toolName is a server's tool as UGT serves it: prefixed, and shortened with a
// digest when it would be too long.
func toolName(prefix, tool string) string {
	name := prefix + "_" + toolSafe(tool)
	if len(name) <= maxTool {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return name[:maxTool-9] + "_" + hex.EncodeToString(sum[:4])
}
