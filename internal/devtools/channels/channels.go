// Package channels writes a release's manifests for the places that install
// ugt: the Homebrew formula for auscaster/homebrew-tap, the Scoop manifest for
// auscaster/scoop-bucket, winget's manifests for microsoft/winget-pkgs, and the
// MCP Registry's entry for its MCP bundles. Each comes from the release's own
// files (ugt from the signed list, the bundles from SHA256SUMS), so every
// address and digest in them is the release's and none is written by hand.
package channels

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/usegeekthing/ugt/internal/devtools/mcpb"
	"github.com/usegeekthing/ugt/internal/devtools/release"
	domain "github.com/usegeekthing/ugt/internal/domain/release"
	"github.com/usegeekthing/ugt/internal/surface/mcp"
)

const (
	repository = "usegeekthing/ugt"
	homepage   = "https://usegeekthing.com"
	license    = "Apache-2.0"
	// summary is the line every manager shows. Homebrew takes at most 80
	// characters, not starting with the formula's name.
	summary = "Get open-source GitHub projects running on your computer, with your AI app"
	// WingetID is ugt's identifier in winget, which ugt update names to a
	// winget install.
	WingetID = "auscaster.ugt"
	// wingetSchema is the version of winget's manifest format written.
	wingetSchema = "1.10.0"
)

// Render returns the manifests for the release in list, whose files have the
// digests in sums (by file name, as SHA256SUMS gives them), by the path each
// is written to.
func Render(list domain.List, sums map[string]string) (map[string]string, error) {
	var macArm, macIntel, linuxArm, linuxIntel, windowsIntel, windowsArm domain.Asset
	for _, want := range []struct {
		asset        *domain.Asset
		goos, goarch string
	}{
		{&macArm, "darwin", "arm64"}, {&macIntel, "darwin", "amd64"},
		{&linuxArm, "linux", "arm64"}, {&linuxIntel, "linux", "amd64"},
		{&windowsIntel, "windows", "amd64"}, {&windowsArm, "windows", "arm64"},
	} {
		a, ok := list.Assets[domain.Platform(want.goos, want.goarch)]
		if !ok || a.Name != domain.Binary(want.goos, want.goarch) {
			return nil, fmt.Errorf("the release has no ugt for %s", domain.Platform(want.goos, want.goarch))
		}
		*want.asset = a
	}
	url := func(a domain.Asset) string {
		return fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", repository, list.Version, a.Name)
	}
	files := map[string]string{}

	files["homebrew/ugt.rb"] = fmt.Sprintf(`class Ugt < Formula
  desc %q
  homepage %q
  license %q

  on_macos do
    on_arm do
      url %q
      sha256 %q
    end
    on_intel do
      url %q
      sha256 %q
    end
  end

  on_linux do
    on_arm do
      url %q
      sha256 %q
    end
    on_intel do
      url %q
      sha256 %q
    end
  end

  def install
    bin.install Dir["ugt-*"].first => "ugt"
  end

  test do
    assert_match "ugt #{version}", shell_output("#{bin}/ugt version")
  end
end
`, summary, homepage, license,
		url(macArm), macArm.SHA256, url(macIntel), macIntel.SHA256,
		url(linuxArm), linuxArm.SHA256, url(linuxIntel), linuxIntel.SHA256)

	// Scoop names the download ugt.exe by the fragment after #/.
	scoop, err := json.MarshalIndent(map[string]any{
		"version":     list.Version,
		"description": summary,
		"homepage":    homepage,
		"license":     license,
		"architecture": map[string]any{
			"64bit": map[string]string{"url": url(windowsIntel) + "#/ugt.exe", "hash": windowsIntel.SHA256},
			"arm64": map[string]string{"url": url(windowsArm) + "#/ugt.exe", "hash": windowsArm.SHA256},
		},
		"bin": "ugt.exe",
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	files["scoop/ugt.json"] = string(scoop) + "\n"

	// winget's manifests sit where winget-pkgs keeps them, so a submission
	// copies the folder as it is.
	dir := fmt.Sprintf("winget/manifests/%s/%s/%s/", strings.ToLower(WingetID[:1]), strings.ReplaceAll(WingetID, ".", "/"), list.Version)
	head := func(kind string) string {
		return fmt.Sprintf("# yaml-language-server: $schema=https://aka.ms/winget-manifest.%s.%s.schema.json\n\nPackageIdentifier: %s\nPackageVersion: %s\n", kind, wingetSchema, WingetID, list.Version)
	}
	tail := func(kind string) string {
		return fmt.Sprintf("ManifestType: %s\nManifestVersion: %s\n", kind, wingetSchema)
	}
	files[dir+WingetID+".yaml"] = head("version") + "DefaultLocale: en-US\n" + tail("version")
	files[dir+WingetID+".locale.en-US.yaml"] = head("defaultLocale") + fmt.Sprintf(`PackageLocale: en-US
Publisher: auscaster
PublisherUrl: https://auscaster.com
PublisherSupportUrl: https://github.com/%[1]s/issues
PackageName: UGT
PackageUrl: %[2]s
License: %[3]s
LicenseUrl: https://github.com/%[1]s/blob/main/LICENSE
ShortDescription: %[4]s
Moniker: ugt
ReleaseNotesUrl: https://github.com/%[1]s/releases/tag/v%[5]s
`, repository, homepage, license, summary, list.Version) + tail("defaultLocale")
	files[dir+WingetID+".installer.yaml"] = head("installer") + fmt.Sprintf(`InstallerType: portable
Commands:
  - ugt
Installers:
  - Architecture: x64
    InstallerUrl: %s
    InstallerSha256: %s
  - Architecture: arm64
    InstallerUrl: %s
    InstallerSha256: %s
`, url(windowsIntel), strings.ToUpper(windowsIntel.SHA256), url(windowsArm), strings.ToUpper(windowsArm.SHA256)) + tail("installer")
	server, err := registry(list.Version, sums)
	if err != nil {
		return nil, err
	}
	files["registry/server.json"] = server
	return files, nil
}

// registryName is UGT's name in the MCP Registry, proved by a DNS record at
// usegeekthing.com.
const registryName = "com.usegeekthing/ugt"

// hostedMCP is UGT's hosted MCP server, for AI apps with no UGT yet: it finds
// how UGT runs a project and sets UGT up. It is the entry's remote, so UGT is
// one server in the registry, whichever way an AI app connects.
const hostedMCP = "https://mcp.usegeekthing.com/mcp"

// registry is the MCP Registry's entry for the release: its MCP bundle for
// every computer, by address and digest, and the hosted server.
func registry(version string, sums map[string]string) (string, error) {
	var packages []map[string]any
	for _, p := range release.Platforms {
		name := mcpb.Name(p[0], p[1])
		sum, ok := sums[name]
		if !ok {
			return "", fmt.Errorf("the release has no MCP bundle for %s", domain.Platform(p[0], p[1]))
		}
		packages = append(packages, map[string]any{
			"registryType": "mcpb",
			"identifier":   fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", repository, version, name),
			"version":      version,
			"fileSha256":   sum,
			"transport":    map[string]string{"type": "stdio"},
		})
	}
	data, err := json.MarshalIndent(map[string]any{
		"$schema":     "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
		"name":        registryName,
		"title":       "UGT: Use the Geek Thing",
		"description": summary,
		"version":     version,
		"websiteUrl":  homepage,
		"repository":  map[string]string{"url": "https://github.com/" + repository, "source": "github"},
		"icons":       mcp.Icons,
		"packages":    packages,
		"remotes":     []map[string]string{{"type": "streamable-http", "url": hostedMCP}},
	}, "", "  ")
	return string(data) + "\n", err
}
