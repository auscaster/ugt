package releases

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/domain/release"
)

// fakeGitHub publishes one release, v0.3.0, as GitHub's release pages do.
func fakeGitHub(t *testing.T, listed string, files map[string][]byte) (*Releases, string) {
	t.Helper()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	list := release.List{Version: listed, Assets: map[string]release.Asset{}}
	for name, data := range files {
		sum := sha256.Sum256(data)
		list.Assets[name] = release.Asset{Name: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	}
	listed2, _ := json.Marshal(list)
	served := map[string][]byte{"/download/v0.3.0/" + release.ListFile: listed2, "/download/v0.3.0/" + release.SignatureFile: release.Sign(listed2, private)}
	for name, data := range files {
		served["/download/v0.3.0/"+name] = data
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/latest" {
			http.Redirect(w, req, "/auscaster/ugt/releases/tag/v0.3.0", http.StatusFound)
			return
		}
		if data, ok := served[req.URL.Path]; ok {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, req)
	}))
	t.Cleanup(srv.Close)
	self := filepath.Join(t.TempDir(), "ugt")
	if err := os.WriteFile(self, []byte("the old ugt"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Releases{HTTP: srv.Client(), Base: srv.URL, Self: self, Key: base64.StdEncoding.EncodeToString(public)}, srv.URL
}

// The newest release's file replaces this ugt, checked against its list.
func TestInstallsTheNewestRelease(t *testing.T) {
	r, _ := fakeGitHub(t, "0.3.0", map[string][]byte{"ugt-test": []byte("the new ugt")})
	l, err := r.Latest(context.Background())
	if err != nil || l.Version != "0.3.0" {
		t.Fatalf("%+v, %v", l, err)
	}
	got, err := r.Install(context.Background(), l.Version, l.Assets["ugt-test"])
	if err != nil || got != r.Self {
		t.Fatalf("%s, %v", got, err)
	}
	if data, _ := os.ReadFile(r.Self); string(data) != "the new ugt" {
		t.Errorf("ugt is %q", data)
	}
	if info, _ := os.Stat(r.Self); runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Errorf("not executable: %v", info.Mode())
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(r.Self), ".ugt-update-*")); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}

// A download that is not the listed file never replaces ugt, nor does a list
// that is not the release it was published as.
func TestRefusesWhatTheListDoesNotVouchFor(t *testing.T) {
	r, _ := fakeGitHub(t, "0.3.0", map[string][]byte{"ugt-test": []byte("the new ugt")})
	l, _ := r.Latest(context.Background())
	tampered := l.Assets["ugt-test"]
	tampered.SHA256 = strings.Repeat("0", 64)
	if _, err := r.Install(context.Background(), l.Version, tampered); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("got %v", err)
	}
	if data, _ := os.ReadFile(r.Self); string(data) != "the old ugt" {
		t.Errorf("ugt is %q", data)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(r.Self), ".ugt-update-*")); len(left) != 0 {
		t.Errorf("left %v", left)
	}

	r, _ = fakeGitHub(t, "0.2.9", map[string][]byte{"ugt-test": []byte("x")})
	if _, err := r.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "lists itself as 0.2.9") {
		t.Errorf("a list under another release's tag: %v", err)
	}
}

func TestManager(t *testing.T) {
	for self, want := range map[string]string{
		"/opt/homebrew/Cellar/ugt/0.3.0/bin/ugt": "brew upgrade ugt",
		"/home/me/.linuxbrew/bin/ugt":            "brew upgrade ugt",
		`C:\Users\me\AppData\Local\Microsoft\WinGet\Packages\auscaster.ugt_Microsoft.Winget.Source_8wekyb3d8bbwe\ugt.exe`: "winget upgrade auscaster.ugt",
		`C:\Program Files\WinGet\Packages\auscaster.ugt_Microsoft.Winget.Source_8wekyb3d8bbwe\ugt.exe`:                    "winget upgrade auscaster.ugt",
		`C:\Users\me\AppData\Local\Microsoft\WinGet\Links\ugt.exe`:                                                        "winget upgrade auscaster.ugt",
		`D:\Tools\auscaster.ugt_Microsoft.Winget.Source_8wekyb3d8bbwe\ugt.exe`:                                            "winget upgrade auscaster.ugt",
		"/Users/me/.local/bin/ugt": "",
	} {
		if got := (&Releases{Self: self}).Manager(); got != want {
			t.Errorf("%s: %q", self, got)
		}
	}
}

// Scoop's ugt is known by the install.json Scoop writes beside it, wherever
// Scoop's root is.
func TestManagerKnowsScoopAnywhere(t *testing.T) {
	version := filepath.Join(t.TempDir(), "Applications", "apps", "ugt", "0.3.0")
	self := filepath.Join(version, "ugt.exe")
	if err := os.MkdirAll(version, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := (&Releases{Self: self}).Manager(); got != "" {
		t.Errorf("without Scoop's record: %q", got)
	}
	if err := os.WriteFile(filepath.Join(version, "install.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := (&Releases{Self: self}).Manager(); got != "scoop update ugt" {
		t.Errorf("Scoop's ugt: %q", got)
	}
}

// What starts ugt again keeps the path its package manager keeps through
// upgrades, never the folder of the version running.
func TestStableIsThePathThatOutlivesAnUpgrade(t *testing.T) {
	root := t.TempDir()
	touch := func(parts ...string) string {
		p := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cellar := touch("brew", "Cellar", "ugt", "0.3.0", "bin", "ugt")
	if got := Stable(cellar); got != cellar {
		t.Errorf("without Homebrew's opt link: %s", got)
	}
	opt := touch("brew", "opt", "ugt", "bin", "ugt")
	scoop := touch("scoop", "apps", "ugt", "0.3.0", "ugt.exe")
	touch("scoop", "apps", "ugt", "0.3.0", "install.json")
	current := touch("scoop", "apps", "ugt", "current", "ugt.exe")
	own := touch("home", ".local", "bin", "ugt")
	for self, want := range map[string]string{cellar: opt, scoop: current, own: own} {
		if got := Stable(self); got != want {
			t.Errorf("%s: %s, want %s", self, got, want)
		}
	}
}
