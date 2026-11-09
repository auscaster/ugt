// Package release writes a release's list of files, signed: what ugt update
// checks before it replaces itself. It lists ugt for every computer UGT
// releases for, with their digests, and SHA256SUMS of every file for the
// install scripts.
package release

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/usegeekthing/ugt/internal/domain/release"
)

// Platforms are the computers every release has ugt for.
var Platforms = [][2]string{
	{"darwin", "arm64"}, {"darwin", "amd64"},
	{"linux", "amd64"}, {"linux", "arm64"},
	{"windows", "amd64"}, {"windows", "arm64"},
}

// SumsFile is the digest of every file in a release, as sha256sum writes it.
const SumsFile = "SHA256SUMS"

// Key reads a release key's private half: its seed, in base64.
func Key(seed string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(seed))
	if err != nil || len(b) != ed25519.SeedSize {
		return nil, errors.New("the release key is not an ed25519 seed in base64")
	}
	return ed25519.NewKeyFromSeed(b), nil
}

// Write lists the release's ugt for every platform in dir, signs the list
// with key, checks the signature with public, the key every ugt carries,
// and writes SHA256SUMS of every file in dir.
func Write(dir, version string, key ed25519.PrivateKey, public string) error {
	if !release.Valid(version) {
		return fmt.Errorf("%q is not a semantic version", version)
	}
	list := release.List{Version: version, Assets: map[string]release.Asset{}}
	for _, p := range Platforms {
		name := release.Binary(p[0], p[1])
		sum, size, err := digest(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("ugt for %s: %w", release.Platform(p[0], p[1]), err)
		}
		list.Assets[release.Platform(p[0], p[1])] = release.Asset{Name: name, SHA256: sum, Size: size}
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	sig := release.Sign(data, key)
	if _, err := release.ParseWith(data, sig, public); err != nil {
		return fmt.Errorf("the signed list does not check with UGT's release key (%w): is this the release key?", err)
	}
	if err := os.WriteFile(filepath.Join(dir, release.ListFile), data, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, release.SignatureFile), sig, 0o644); err != nil {
		return err
	}
	return writeSums(dir)
}

func digest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

func writeSums(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var lines []string
	for _, e := range entries {
		if e.IsDir() || e.Name() == SumsFile {
			continue
		}
		sum, _, err := digest(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		lines = append(lines, sum+"  "+e.Name())
	}
	sort.Strings(lines)
	return os.WriteFile(filepath.Join(dir, SumsFile), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
