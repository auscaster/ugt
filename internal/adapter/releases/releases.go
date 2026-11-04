// Package releases is ugt's releases on GitHub, and the ugt on this computer.
// It implements the Releases port: the newest release's list, used only when
// UGT's release key signed it, and a release's file put in place of this ugt,
// used only when its digest is the list's.
package releases

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/release"
)

// Base is where UGT's releases are published.
const Base = "https://github.com/auscaster/ugt/releases"

// Releases reads releases from Base and replaces the ugt at Self.
type Releases struct {
	lockFile func(string, bool) (func(), error)
	HTTP     *http.Client
	Base     string
	Self     string // this ugt, its links followed
	Key      string // UGT's release key; tests sign with their own
}

var _ app.Releases = (*Releases)(nil)

// New is UGT's releases, for the ugt at self.
func New(self string, lockFile func(string, bool) (func(), error)) *Releases {
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	return &Releases{lockFile: lockFile, HTTP: &http.Client{Timeout: 15 * time.Minute}, Base: Base, Self: self, Key: release.Key}
}

// Latest is the newest release's list. It reads the newest release's tag
// first, so the list and its signature come from one release, even while a
// newer one is being published.
func (r *Releases) Latest(ctx context.Context) (release.List, error) {
	tag, err := r.latestTag(ctx)
	if err != nil {
		return release.List{}, err
	}
	list, err := r.get(ctx, r.Base+"/download/"+tag+"/"+release.ListFile)
	if err != nil {
		return release.List{}, err
	}
	sig, err := r.get(ctx, r.Base+"/download/"+tag+"/"+release.SignatureFile)
	if err != nil {
		return release.List{}, err
	}
	l, err := release.ParseWith(list, sig, r.Key)
	if err != nil {
		return release.List{}, err
	}
	if "v"+l.Version != tag {
		return release.List{}, fmt.Errorf("release %s lists itself as %s: not using it", tag, l.Version)
	}
	return l, nil
}

// latestTag is the newest release's tag, from where GitHub sends /latest.
func (r *Releases) latestTag(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.Base+"/latest", nil)
	if err != nil {
		return "", err
	}
	client := *r.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("reach UGT's releases: %w", err)
	}
	resp.Body.Close()
	tag := path.Base(resp.Header.Get("Location"))
	if resp.StatusCode/100 != 3 || !strings.HasPrefix(tag, "v") {
		return "", fmt.Errorf("UGT's releases have no newest release (%s)", resp.Status)
	}
	return tag, nil
}

func (r *Releases) get(ctx context.Context, url string) ([]byte, error) {
	resp, err := r.download(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func (r *Releases) download(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return resp, nil
}

// Install downloads the release's file beside this ugt, checks it against
// the list, and puts it in this ugt's place in one step, so a download that
// fails or does not match leaves this ugt as it was.
func (r *Releases) Install(ctx context.Context, version string, asset release.Asset) (string, error) {
	dir := filepath.Dir(r.Self)
	cleanOld(r.Self)
	tmp, err := os.CreateTemp(dir, ".ugt-update-*")
	if err != nil {
		return "", fmt.Errorf("write beside %s: %w", r.Self, err)
	}
	defer os.Remove(tmp.Name()) // gone once renamed; removed if anything failed
	resp, err := r.download(ctx, r.Base+"/download/v"+version+"/"+asset.Name)
	if err != nil {
		tmp.Close()
		return "", err
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, sum), resp.Body)
	resp.Body.Close()
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("download %s: %w", asset.Name, err)
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != asset.SHA256 || (asset.Size > 0 && n != asset.Size) {
		return "", fmt.Errorf("the download of %s does not match the release list: not using it", asset.Name)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", err
	}
	if err := replace(tmp.Name(), r.Self); err != nil {
		return "", fmt.Errorf("put UGT %s in place of %s: %w", version, r.Self, err)
	}
	return r.Self, nil
}

// Lock keeps any other ugt from updating this one until unlocked, by a lock
// file beside it.
func (r *Releases) Lock() (func(), error) {
	return r.lockFile(filepath.Join(filepath.Dir(r.Self), ".ugt-update.lock"), true)
}

// wingetID is ugt's package in winget. winget keeps a portable package in a
// folder named <package>_<source>, under WinGet\Packages for one user or the
// whole machine, or under a root the user chose, and links it from
// WinGet\Links; one installed into a folder the user named is known by
// winget's own record of it.
const wingetID = "auscaster.ugt"

// Manager is the package manager that installed this ugt, by where it is.
func (r *Releases) Manager() string {
	p := strings.ToLower(strings.ReplaceAll(r.Self, `\`, "/"))
	switch {
	case strings.Contains(p, "/cellar/") || strings.Contains(p, "/homebrew/") || strings.Contains(p, "linuxbrew/"):
		return "brew upgrade ugt"
	case strings.Contains(p, "/winget/") || strings.HasPrefix(path.Base(path.Dir(p)), wingetID+"_") || wingetRecords(r.Self):
		return "winget upgrade " + wingetID
	case scoopVersion(r.Self):
		return "scoop update ugt"
	}
	return ""
}

// scoopVersion reports whether self is in a version's folder of Scoop's ugt,
// <root>/apps/ugt/<version>, which Scoop marks with its install.json wherever
// its root is.
func scoopVersion(self string) bool {
	app := filepath.Dir(filepath.Dir(self))
	return filepath.Base(app) == "ugt" && filepath.Base(filepath.Dir(app)) == "apps" &&
		exists(filepath.Join(filepath.Dir(self), "install.json"))
}

// Stable is the path a package manager keeps for this ugt across upgrades,
// which remove the folder of the version running: Homebrew's opt link for its
// Cellar, Scoop's current for its version's folder. Anything that starts ugt
// again, such as an AI app's MCP connection, keeps this path; any other ugt's
// path is its own.
func Stable(self string) string {
	slash := filepath.ToSlash(self)
	if i := strings.Index(slash, "/Cellar/ugt/"); i >= 0 {
		version := slash[i+len("/Cellar/ugt/"):]
		if j := strings.Index(version, "/"); j > 0 {
			if opt := filepath.FromSlash(slash[:i] + "/opt/ugt" + version[j:]); exists(opt) {
				return opt
			}
		}
	}
	if scoopVersion(self) {
		if current := filepath.Join(filepath.Dir(filepath.Dir(self)), "current", filepath.Base(self)); exists(current) {
			return current
		}
	}
	return self
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
