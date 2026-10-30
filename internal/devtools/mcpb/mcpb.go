// Package mcpb builds UGT's MCP bundles: the .mcpb files Claude Desktop
// installs with one click, one per operating system and CPU, named
// ugt-mcp-<os>-<arch>.mcpb as the MCP Registry requires. The manifest's tool
// list is read from the MCP server itself, so it always matches the binary.
package mcpb

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/surface/mcp"
)

// Name is a bundle's file name.
func Name(goos, goarch string) string { return fmt.Sprintf("ugt-mcp-%s-%s.mcpb", goos, goarch) }

// platforms maps Go's operating system names to the manifest's.
var platforms = map[string]string{"darwin": "darwin", "windows": "win32", "linux": "linux"}

// Tool is one tool, as the manifest lists it.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Tools lists the MCP server's tools by asking it, over an in-memory
// connection.
func Tools(ctx context.Context) ([]Tool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &mcp.Server{App: &app.App{Now: time.Now}, Version: "manifest"}
	serverT, clientT := sdk.NewInMemoryTransports()
	go func() { _ = server.Serve(ctx, serverT) }()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "mcpb"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	var tools []Tool
	for _, t := range list.Tools {
		tools = append(tools, Tool{Name: t.Name, Description: t.Description})
	}
	return tools, nil
}

// Manifest is a bundle's manifest.json.
func Manifest(goos, version string, tools []Tool) ([]byte, error) {
	entry := "server/ugt"
	if goos == "windows" {
		entry += ".exe"
	}
	m := map[string]any{
		"manifest_version": "0.3",
		"name":             "ugt",
		"display_name":     "UGT: Use the GitHub Thing",
		"version":          version,
		"description":      "Get GitHub projects working on your computer and do your task with them. Say \"Use the GitHub Thing\", with a link and what you want.",
		"long_description": "UGT runs a GitHub project's recipe in a private container on your computer: it builds what it needs, does your task on your files, checks the result and puts it in your UGT folder. With no recipe yet, your AI makes one and UGT verifies it. On Windows it sets up its own engine in WSL; elsewhere it uses the one you have.",
		"author":           map[string]any{"name": "auscaster", "url": "https://auscaster.com/ugt"},
		"homepage":         "https://auscaster.com/ugt",
		"repository":       map[string]any{"type": "git", "url": "https://github.com/auscaster/ugt"},
		"license":          "Apache-2.0",
		"keywords":         []string{"github", "docker", "containers", "recipes"},
		"server": map[string]any{
			"type":        "binary",
			"entry_point": entry,
			// apps add .exe on Windows themselves
			"mcp_config": map[string]any{"command": "${__dirname}/server/ugt", "args": []string{"mcp"}, "env": map[string]any{}},
		},
		"tools":         tools,
		"compatibility": map[string]any{"platforms": []string{platforms[goos]}},
	}
	return json.MarshalIndent(m, "", "  ")
}

// Build writes a bundle to out: the manifest and the binary for goos.
func Build(out io.Writer, binary, goos, version string, tools []Tool) error {
	manifest, err := Manifest(goos, version, tools)
	if err != nil {
		return err
	}
	bin, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	z := zip.NewWriter(out)
	entry := "server/ugt"
	if goos == "windows" {
		entry += ".exe"
	}
	for _, f := range []struct {
		name string
		data []byte
		mode os.FileMode
	}{{"manifest.json", manifest, 0o644}, {entry, bin, 0o755}} {
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		h.SetMode(f.mode)
		w, err := z.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := w.Write(f.data); err != nil {
			return err
		}
	}
	return z.Close()
}
