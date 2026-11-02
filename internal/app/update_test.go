package app

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/domain/release"
)

type fakeReleases struct {
	latest    release.List
	manager   string
	installed []string
}

func (f *fakeReleases) Latest(context.Context) (release.List, error) { return f.latest, nil }
func (f *fakeReleases) Manager() string                              { return f.manager }
func (f *fakeReleases) Lock() (func(), error)                        { return func() {}, nil }
func (f *fakeReleases) Install(_ context.Context, version string, a release.Asset) (string, error) {
	f.installed = append(f.installed, version+" "+a.Name)
	return "/opt/ugt/" + a.Name, nil
}

// Only a newer release for this computer replaces ugt; a development build
// and a package manager's ugt are left as they are.
func TestUpdate(t *testing.T) {
	here := release.Platform(runtime.GOOS, runtime.GOARCH)
	newer := release.List{Version: "0.3.0", Assets: map[string]release.Asset{here: {Name: "ugt-new"}}}
	ctx := context.Background()

	r := &fakeReleases{latest: newer}
	res, err := (&App{Version: "0.2.0", Releases: r}).Update(ctx)
	if err != nil || !res.Updated || res.Path != "/opt/ugt/ugt-new" || strings.Join(r.installed, ",") != "0.3.0 ugt-new" {
		t.Fatalf("an older ugt: %+v, %v, %v", res, err, r.installed)
	}

	r = &fakeReleases{latest: newer}
	if res, err := (&App{Version: "0.3.0", Releases: r}).Update(ctx); err != nil || res.Updated || res.Latest != "0.3.0" || len(r.installed) != 0 {
		t.Errorf("the newest ugt: %+v, %v", res, err)
	}
	r = &fakeReleases{latest: release.List{Version: "0.3.0", Assets: map[string]release.Asset{"plan9-mips": {Name: "x"}}}}
	if _, err := (&App{Version: "0.2.0", Releases: r}).Update(ctx); err == nil || !strings.Contains(err.Error(), "no release for "+here) {
		t.Errorf("no file for this computer: %v", err)
	}
	r = &fakeReleases{latest: newer}
	if _, err := (&App{Version: "dev", Releases: r}).Update(ctx); err == nil || !strings.Contains(err.Error(), "development build") || len(r.installed) != 0 {
		t.Errorf("a development build: %v", err)
	}
	r = &fakeReleases{latest: newer, manager: "brew upgrade ugt"}
	if res, err := (&App{Version: "0.2.0", Releases: r}).Update(ctx); err != nil || res.Next != "brew upgrade ugt" || len(r.installed) != 0 {
		t.Errorf("Homebrew's ugt: %+v, %v", res, err)
	}
}
