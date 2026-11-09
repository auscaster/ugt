// Package manifests_test holds UGT's plugin manifests to the MCP server's own
// metadata and to each other, so a hand edit to one cannot leave the others,
// or what the server says, behind.
package manifests_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/auscaster/ugt/assets"
	"github.com/auscaster/ugt/internal/surface/mcp"
)

type manifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	DisplayName string   `json:"displayName"`
	Homepage    string   `json:"homepage"`
	Keywords    []string `json:"keywords"`
	Icon        string   `json:"icon"`
	Logo        string   `json:"logo"`
	Privacy     string   `json:"privacyPolicyUrl"`
	Terms       string   `json:"termsOfServiceUrl"`
	Extensions  struct {
		OpenAI struct {
			Interface struct {
				DisplayName string `json:"displayName"`
				Logo        string `json:"logo"`
				Composer    string `json:"composerIcon"`
				Website     string `json:"websiteURL"`
				Privacy     string `json:"privacyPolicyURL"`
				Terms       string `json:"termsOfServiceURL"`
			} `json:"interface"`
		} `json:"com.openai"`
	} `json:"extensions"`
}

const root = "../../.."

func read(t *testing.T, file string) manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return m
}

func TestPluginManifestsAgreeWithTheServer(t *testing.T) {
	const title = "UGT: Use the Geek Thing"
	claude, cursor, agents, gemini := read(t, ".claude-plugin/plugin.json"), read(t, ".cursor-plugin/plugin.json"), read(t, "plugin.json"), read(t, "gemini-extension.json")
	codex := agents.Extensions.OpenAI.Interface

	for file, m := range map[string]manifest{".claude-plugin": claude, ".cursor-plugin": cursor, "plugin.json": agents, "gemini-extension.json": gemini} {
		if m.Name != "ugt" || m.Version != claude.Version {
			t.Errorf("%s names %q %q; every manifest is ugt at one version", file, m.Name, m.Version)
		}
	}
	for file, name := range map[string]string{".claude-plugin": claude.DisplayName, ".cursor-plugin": cursor.DisplayName, "plugin.json's Codex interface": codex.DisplayName} {
		if name != title {
			t.Errorf("%s shows %q, not %q", file, name, title)
		}
	}
	for file, site := range map[string]string{".claude-plugin": claude.Homepage, ".cursor-plugin": cursor.Homepage, "plugin.json": agents.Homepage, "plugin.json's Codex interface": codex.Website} {
		if site != mcp.Website {
			t.Errorf("%s names %q, not the server's site %q", file, site, mcp.Website)
		}
	}
	if !slices.Equal(claude.Keywords, cursor.Keywords) || !slices.Equal(claude.Keywords, agents.Keywords) || len(claude.Keywords) == 0 {
		t.Errorf("keywords differ: %v, %v, %v", claude.Keywords, cursor.Keywords, agents.Keywords)
	}
	if claude.Privacy != codex.Privacy || claude.Terms != codex.Terms || claude.Privacy != mcp.Website+"/privacy" || claude.Terms != mcp.Website+"/terms" {
		t.Errorf("privacy and terms differ: %q %q, %q %q", claude.Privacy, claude.Terms, codex.Privacy, codex.Terms)
	}
	// every icon a manifest names is UGT's red tile, the one the bundle ships
	for file, icon := range map[string]string{".claude-plugin": claude.Icon, ".cursor-plugin": cursor.Logo, "Codex logo": codex.Logo, "Codex composer icon": codex.Composer} {
		data, err := os.ReadFile(filepath.Join(root, filepath.Clean(icon)))
		if err != nil || !bytes.Equal(data, assets.Icon) {
			t.Errorf("%s names %q, which is not UGT's icon (%v)", file, icon, err)
		}
	}
}
