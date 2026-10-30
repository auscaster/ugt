package mcp

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/catalogue"
)

// catalogueDir serves the spec's example job as example/tool/markdown.
type catalogueDir struct{}

const exampleJob = "../../../spec/recipe/v0/examples/valid/example-job"

func (catalogueDir) Source() catalogue.Source { return catalogue.Published }
func (catalogueDir) Project(_ context.Context, owner, repo string) ([]catalogue.Entry, error) {
	if owner+"/"+repo != "example/tool" {
		return nil, nil
	}
	return []catalogue.Entry{catalogue.FromFolder(os.DirFS(exampleJob), "example/tool/markdown", catalogue.Published)}, nil
}
func (catalogueDir) Search(context.Context, []string) ([]catalogue.Entry, error) { return nil, nil }
func (catalogueDir) Open(context.Context, string) (fs.FS, error)                 { return os.DirFS(exampleJob), nil }

type readDisk struct{}

func (readDisk) FS(dir string) fs.FS            { return os.DirFS(dir) }
func (readDisk) WriteFile(string, []byte) error { return fs.ErrPermission }
func (readDisk) RemoveAll(string) error         { return fs.ErrPermission }
func (readDisk) Exists(p string) (bool, error)  { _, err := os.Stat(p); return err == nil, nil }

func connect(t *testing.T) *sdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := &app.App{Catalogues: []app.Catalogue{catalogueDir{}}, Disk: readDisk{}, Now: time.Now}
	s := &Server{App: a, Version: "test", Wait: time.Second}
	serverT, clientT := sdk.NewInMemoryTransports()
	go func() { _ = s.server(ctx).Run(ctx, serverT) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

// The locked tool names, and the routing an AI app is given on connecting.
func TestToolsAndInstructions(t *testing.T) {
	session := connect(t)
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
	}
	want := []string{"ugt_delete_data", "ugt_doctor", "ugt_exec", "ugt_find", "ugt_list", "ugt_new", "ugt_remove", "ugt_run", "ugt_setup", "ugt_status", "ugt_stop", "ugt_submit", "ugt_verify"}
	slices.Sort(names)
	if !slices.Equal(names, want) {
		t.Errorf("tools %v, want %v", names, want)
	}
	init := session.InitializeResult()
	if !strings.Contains(init.Instructions, `"Use the GitHub Thing"`) || !strings.Contains(init.Instructions, "ugt_find") {
		t.Errorf("instructions do not route the phrase: %q", init.Instructions)
	}
}

func TestFindAndNoRecipe(t *testing.T) {
	session := connect(t)
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		res, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("%s failed: %+v", name, res.Content)
		}
		data, _ := json.Marshal(res.StructuredContent)
		var out map[string]any
		_ = json.Unmarshal(data, &out)
		return out
	}
	found := call("ugt_find", map[string]any{"query": "https://github.com/example/tool"})
	if recipes, _ := found["recipes"].([]any); len(recipes) != 1 {
		t.Fatalf("find %v", found)
	}
	none := call("ugt_run", map[string]any{"project": "someone/else"})
	if none["state"] != "no-recipe" || !strings.Contains(none["next"].(string), "ugt recipe new someone/else") {
		t.Errorf("run with no recipe %v", none)
	}
	res, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: "ugt_delete_data", Arguments: map[string]any{"recipe": "a/b/c", "user_agreed": false}})
	if err != nil || !res.IsError {
		t.Errorf("delete without agreement must be refused: %+v, %v", res, err)
	}
}
