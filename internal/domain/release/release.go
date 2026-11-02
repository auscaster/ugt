// Package release is a release of ugt as UGT publishes it: the list of its
// files for each computer, with their digests, signed with UGT's release key.
// Every ugt carries the key's public half, so it only ever updates to a
// release UGT made, whoever serves the files.
package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Key is the public half of UGT's release key. The release workflow signs
// each release's list with the private half.
const Key = "ZuWNnc7KvZVthZD1wEVDauOmnzXgpy7O/FllIovHrtI="

// ListFile and SignatureFile are the list and its signature, published with
// every release.
const (
	ListFile      = "release.json"
	SignatureFile = "release.json.sig"
)

// List is a release: its version and ugt for each computer.
type List struct {
	Version string           `json:"version"` // semantic, without a leading v
	Assets  map[string]Asset `json:"assets"`  // by Platform
}

// Asset is one file of a release.
type Asset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Platform names a computer as a list does: darwin-arm64, windows-amd64.
func Platform(goos, goarch string) string { return goos + "-" + goarch }

// Binary is the file name of ugt for a computer in a release.
func Binary(goos, goarch string) string {
	name := "ugt-" + Platform(goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// Sign signs a list with the private half of a release key: the release
// workflow's step. The signature is base64, one line.
func Sign(list []byte, private ed25519.PrivateKey) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(private, list)) + "\n")
}

// Parse returns the list in data, if UGT's release key signed it.
func Parse(data, signature []byte) (List, error) {
	return ParseWith(data, signature, Key)
}

// ParseWith returns the list in data, if key, a public key in base64, signed
// it.
func ParseWith(data, signature []byte, key string) (List, error) {
	public, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return List{}, errors.New("UGT's release key is not a key")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signature)))
	if err != nil || !ed25519.Verify(public, data, sig) {
		return List{}, errors.New("the release list is not signed with UGT's release key: not using it")
	}
	var l List
	if err := json.Unmarshal(data, &l); err != nil {
		return List{}, fmt.Errorf("the release list: %w", err)
	}
	if _, ok := parseVersion(l.Version); !ok {
		return List{}, fmt.Errorf("the release list's version %q is not a semantic version", l.Version)
	}
	for platform, a := range l.Assets {
		if sum, err := hex.DecodeString(a.SHA256); err != nil || len(sum) != 32 || a.Name == "" || strings.ContainsAny(a.Name, `/\`) {
			return List{}, fmt.Errorf("the release list's file for %s is not one ugt can check", platform)
		}
	}
	return l, nil
}

// Valid reports whether v is a release's version, as opposed to a
// development build's, such as "dev".
func Valid(v string) bool { _, ok := parseVersion(v); return ok }

// Newer reports whether version a comes after version b. A version that is
// not semantic comes before every one that is.
func Newer(a, b string) bool {
	va, okA := parseVersion(a)
	vb, okB := parseVersion(b)
	if !okA || !okB {
		return okA && !okB
	}
	return compare(va, vb) > 0
}

type version struct {
	core [3]int
	pre  []string // a pre-release, such as rc.1: before its release
}

func parseVersion(s string) (version, bool) {
	var v version
	s = strings.SplitN(s, "+", 2)[0] // build metadata does not order
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return v, false
		}
		v.core[i] = n
	}
	if hasPre {
		if pre == "" {
			return v, false
		}
		v.pre = strings.Split(pre, ".")
	}
	return v, true
}

func compare(a, b version) int {
	for i := range a.core {
		if a.core[i] != b.core[i] {
			return sign(a.core[i] - b.core[i])
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		na, errA := strconv.Atoi(a.pre[i])
		nb, errB := strconv.Atoi(b.pre[i])
		switch {
		case errA == nil && errB == nil && na != nb:
			return sign(na - nb)
		case errA == nil && errB != nil:
			return -1 // numbers order before words
		case errA != nil && errB == nil:
			return 1
		case errA != nil && errB != nil && a.pre[i] != b.pre[i]:
			return strings.Compare(a.pre[i], b.pre[i])
		}
	}
	return sign(len(a.pre) - len(b.pre))
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}
