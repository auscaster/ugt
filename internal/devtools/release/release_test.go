package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usegeekthing/ugt/internal/domain/release"
)

func releaseDir(t *testing.T, skip string) string {
	dir := t.TempDir()
	for _, p := range Platforms {
		if name := release.Binary(p[0], p[1]); name != skip {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("ugt for "+name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

// A release lists ugt for every computer, signed so that the key every ugt
// carries checks it, and the install scripts' digests of every file.
func TestWritesASignedListOfEveryComputer(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	key := base64.StdEncoding.EncodeToString(public)
	dir := releaseDir(t, "")
	if err := Write(dir, "0.2.0", private, key); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, release.ListFile))
	sig, _ := os.ReadFile(filepath.Join(dir, release.SignatureFile))
	l, err := release.ParseWith(data, sig, key)
	if err != nil || l.Version != "0.2.0" || len(l.Assets) != len(Platforms) || l.Assets["windows-arm64"].Name != "ugt-windows-arm64.exe" {
		t.Fatalf("%+v, %v", l, err)
	}
	sums, _ := os.ReadFile(filepath.Join(dir, SumsFile))
	if !strings.Contains(string(sums), l.Assets["darwin-arm64"].SHA256+"  ugt-darwin-arm64\n") || !strings.Contains(string(sums), "  "+release.ListFile+"\n") {
		t.Errorf("sums:\n%s", sums)
	}

	_, other, _ := ed25519.GenerateKey(rand.Reader)
	if err := Write(releaseDir(t, ""), "0.2.0", other, key); err == nil || !strings.Contains(err.Error(), "is this the release key") {
		t.Errorf("another key: %v", err)
	}
	if err := Write(releaseDir(t, "ugt-linux-arm64"), "0.2.0", private, key); err == nil || !strings.Contains(err.Error(), "linux-arm64") {
		t.Errorf("a computer missing: %v", err)
	}
}
