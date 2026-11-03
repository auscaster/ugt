package mcpb

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBundle(t *testing.T) {
	tools, err := Tools(context.Background())
	if err != nil || len(tools) < 13 {
		t.Fatalf("tools %v, %v", tools, err)
	}
	bin := filepath.Join(t.TempDir(), "ugt.exe")
	os.WriteFile(bin, []byte("MZ binary"), 0o755)
	var buf bytes.Buffer
	if err := Build(&buf, bin, "windows", "0.1.0", tools); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]*zip.File{}
	for _, f := range z.File {
		names[f.Name] = f
	}
	if names["manifest.json"] == nil || names["server/ugt.exe"] == nil || names["icon.png"] == nil {
		t.Fatalf("files %v", names)
	}
	r, _ := names["manifest.json"].Open()
	var m struct {
		ManifestVersion string   `json:"manifest_version"`
		Name            string   `json:"name"`
		Icon            string   `json:"icon"`
		Privacy         []string `json:"privacy_policies"`
		Server          struct {
			Type       string `json:"type"`
			EntryPoint string `json:"entry_point"`
			MCPConfig  struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcp_config"`
		} `json:"server"`
		Tools         []Tool `json:"tools"`
		Compatibility struct {
			Platforms []string `json:"platforms"`
		} `json:"compatibility"`
	}
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m.ManifestVersion != "0.3" || m.Name != "ugt" || m.Server.Type != "binary" || m.Server.EntryPoint != "server/ugt.exe" ||
		m.Server.MCPConfig.Command != "${__dirname}/server/ugt" || m.Server.MCPConfig.Args[0] != "mcp" ||
		m.Compatibility.Platforms[0] != "win32" || len(m.Tools) != len(tools) || m.Icon != "icon.png" || len(m.Privacy) != 1 {
		t.Errorf("manifest %+v", m)
	}
	if Name("windows", "arm64") != "ugt-mcp-windows-arm64.mcpb" {
		t.Error("bundle names must contain mcp, for the MCP Registry")
	}
}
