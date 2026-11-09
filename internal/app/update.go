package app

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/usegeekthing/ugt/internal/domain/release"
)

// InstallScript is how a person installs a release of UGT.
const InstallScript = "https://usegeekthing.com/install"

// UpdateResult is what ugt update found and did.
type UpdateResult struct {
	From    string `json:"from"`
	Latest  string `json:"latest,omitempty"`
	Updated bool   `json:"updated"`
	Path    string `json:"path,omitempty"` // the new ugt, once updated
	Next    string `json:"next,omitempty"` // what updates it instead
}

// Update replaces this ugt with the newest release when there is one: only a
// release whose list UGT's release key signed, and only the file the list
// names for this computer, with its digest. A package manager that installed
// ugt updates it instead. One update runs at a time, and finds the newest
// release once it holds the lock, so two never leave an older ugt in place.
func (a *App) Update(ctx context.Context) (UpdateResult, error) {
	res := UpdateResult{From: a.Version}
	if a.Releases == nil {
		return res, errors.New("this ugt cannot update itself")
	}
	if m := a.Releases.Manager(); m != "" {
		res.Next = m
		return res, nil
	}
	if !release.Valid(a.Version) {
		return res, fmt.Errorf("this ugt is a development build (%s), which is updated from its source: install a release with %s", a.Version, InstallScript)
	}
	unlock, err := a.Releases.Lock()
	if err != nil {
		return res, fmt.Errorf("wait for any other update of this ugt: %w", err)
	}
	defer unlock()
	latest, err := a.Releases.Latest(ctx)
	if err != nil {
		return res, err
	}
	res.Latest = latest.Version
	if !release.Newer(latest.Version, a.Version) {
		return res, nil
	}
	platform := release.Platform(runtime.GOOS, runtime.GOARCH)
	asset, ok := latest.Assets[platform]
	if !ok {
		return res, fmt.Errorf("UGT %s has no release for %s", latest.Version, platform)
	}
	if res.Path, err = a.Releases.Install(ctx, latest.Version, asset); err != nil {
		return res, err
	}
	res.Updated = true
	return res, nil
}
