package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

// A list is used only when UGT's key signed exactly it.
func TestOnlyASignedListIsUsed(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	key := base64.StdEncoding.EncodeToString(public)
	list := []byte(`{"version":"0.2.0","assets":{"darwin-arm64":{"name":"ugt-darwin-arm64","sha256":"` + strings.Repeat("ab", 32) + `","size":3}}}`)
	got, err := ParseWith(list, Sign(list, private), key)
	if err != nil || got.Version != "0.2.0" || got.Assets["darwin-arm64"].Name != "ugt-darwin-arm64" {
		t.Fatalf("%+v, %v", got, err)
	}
	changed := []byte(strings.Replace(string(list), "0.2.0", "0.2.1", 1))
	if _, err := ParseWith(changed, Sign(list, private), key); err == nil || !strings.Contains(err.Error(), "not signed") {
		t.Errorf("a changed list: %v", err)
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := ParseWith(list, Sign(list, other), key); err == nil {
		t.Error("a list signed with another key was used")
	}
	path := []byte(`{"version":"0.2.0","assets":{"darwin-arm64":{"name":"../ugt","sha256":"` + strings.Repeat("ab", 32) + `"}}}`)
	if _, err := ParseWith(path, Sign(path, private), key); err == nil {
		t.Error("a file name with a path was used")
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.9", true},
		{"0.10.0", "0.9.0", true},
		{"1.0.0", "1.0.0-rc.2", true},
		{"1.0.0-rc.10", "1.0.0-rc.2", true},
		{"1.0.0-rc.1", "1.0.0-beta", true},
		{"1.0.0-beta", "1.0.0-beta.2", false},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "dev", true},
		{"dev", "0.1.0", false},
		{"0.1.0+build.5", "0.1.0", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
