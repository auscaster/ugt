// Package channels writes a release's manifests for the package managers that
// install ugt: the Homebrew formula for auscaster/homebrew-tap, the Scoop
// manifest for auscaster/scoop-bucket, and winget's manifests for
// microsoft/winget-pkgs. Each comes from the release's signed list alone, so
// every address and digest in them is the release's own and none is written
// by hand.
package channels

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/auscaster/ugt/internal/domain/release"
)

const (
	repository = "auscaster/ugt"
	homepage   = "https://usegithubthing.com"
	license    = "Apache-2.0"
	// summary is the line every manager shows. Homebrew takes at most 80
	// characters, not starting with the formula's name.
	summary = "Get GitHub projects running on your computer, with your AI app"
	// WingetID is ugt's identifier in winget, which ugt update names to a
	// winget install.
	WingetID = "auscaster.ugt"
	// wingetSchema is the version of winget's manifest format written.
	wingetSchema = "1.10.0"
)

// Render returns the manifests for the release in list, by the path each is
// written to.
func Render(list release.List) (map[string]string, error) {
	var macArm, macIntel, linuxArm, linuxIntel, windowsIntel, windowsArm release.Asset
	for _, want := range []struct {
		asset        *release.Asset
		goos, goarch string
	}{
		{&macArm, "darwin", "arm64"}, {&macIntel, "darwin", "amd64"},
		{&linuxArm, "linux", "arm64"}, {&linuxIntel, "linux", "amd64"},
		{&windowsIntel, "windows", "amd64"}, {&windowsArm, "windows", "arm64"},
	} {
		a, ok := list.Assets[release.Platform(want.goos, want.goarch)]
		if !ok || a.Name != release.Binary(want.goos, want.goarch) {
			return nil, fmt.Errorf("the release has no ugt for %s", release.Platform(want.goos, want.goarch))
		}
		*want.asset = a
	}
	url := func(a release.Asset) string {
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
	return files, nil
}
