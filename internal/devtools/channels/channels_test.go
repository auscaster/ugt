package channels

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/devtools/release"
	domain "github.com/auscaster/ugt/internal/domain/release"
)

func list() domain.List {
	l := domain.List{Version: "0.2.1", Assets: map[string]domain.Asset{}}
	for i, p := range release.Platforms {
		l.Assets[domain.Platform(p[0], p[1])] = domain.Asset{Name: domain.Binary(p[0], p[1]), SHA256: strings.Repeat(string(rune('a'+i)), 64), Size: 1}
	}
	return l
}

// Every manager gets each computer's ugt at the release's own address, with
// the digest the signed list gives it, as each manager writes digests.
func TestEveryManagerGetsTheReleasesOwnFiles(t *testing.T) {
	l := list()
	files, err := Render(l)
	if err != nil {
		t.Fatal(err)
	}
	brew := files["homebrew/ugt.rb"]
	winget := files["winget/manifests/a/auscaster/ugt/0.2.1/auscaster.ugt.installer.yaml"]
	var scoop struct {
		Version      string
		Architecture map[string]struct{ URL, Hash string }
	}
	if err := json.Unmarshal([]byte(files["scoop/ugt.json"]), &scoop); err != nil || scoop.Version != "0.2.1" {
		t.Fatalf("scoop: %+v, %v", scoop, err)
	}
	for platform, a := range l.Assets {
		url := "https://github.com/auscaster/ugt/releases/download/v0.2.1/" + a.Name
		switch {
		case strings.HasPrefix(platform, "windows"):
			arch := map[string]string{"windows-amd64": "64bit", "windows-arm64": "arm64"}[platform]
			if got := scoop.Architecture[arch]; got.URL != url+"#/ugt.exe" || got.Hash != a.SHA256 {
				t.Errorf("scoop %s: %+v", platform, got)
			}
			if !strings.Contains(winget, "InstallerUrl: "+url+"\n    InstallerSha256: "+strings.ToUpper(a.SHA256)+"\n") {
				t.Errorf("winget lacks %s", platform)
			}
		default:
			if !strings.Contains(brew, "url \""+url+"\"\n      sha256 \""+a.SHA256+"\"") {
				t.Errorf("homebrew lacks %s", platform)
			}
		}
	}
	if len(summary) > 80 || strings.HasPrefix(strings.ToLower(summary), "ugt") {
		t.Errorf("Homebrew refuses the summary %q", summary)
	}
}

// A release missing a computer's ugt writes no manifests, rather than ones
// that leave that computer out.
func TestAReleaseMissingAComputerWritesNothing(t *testing.T) {
	l := list()
	delete(l.Assets, "linux-arm64")
	if files, err := Render(l); err == nil || files != nil {
		t.Fatalf("%v, %v", files, err)
	}
}
