package app

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/auscaster/ugt/internal/domain/access"
	"github.com/auscaster/ugt/internal/domain/compose"
	"github.com/auscaster/ugt/internal/domain/ledger"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

// Siblings share image tags. Serialize changes to their ownership using the
// same host lock as older helpers. Running services do not hold this lock.
func (a *App) lockRecipe(id recipe.ID) (func(), error) {
	unlock, err := a.Disk.TryLock(filepath.Join(a.Layout.State, "locks", id.Owner, id.Repo, id.Task+".lock"))
	if err != nil {
		return nil, fmt.Errorf("lock workspace for %s: %w", id, err)
	}
	if unlock == nil {
		return nil, fmt.Errorf("%s is already running a request on this computer; wait for it to finish, then try again", id)
	}
	return unlock, nil
}

func (a *App) lockHeld(target string) (string, []ledger.Entry, func(), error) {
	id, _, err := a.held(target)
	if err != nil {
		return "", nil, nil, err
	}
	inst, err := recipe.ParseInstallation(id)
	if err != nil {
		return "", nil, nil, err
	}
	unlock, err := a.lockRecipe(inst.ID)
	if err != nil {
		return "", nil, nil, err
	}
	id, live, err := a.held(id)
	if err != nil {
		unlock()
		return "", nil, nil, err
	}
	return id, live, unlock, nil
}

// A named installation must be able to give the user independent storage
// and ports. Compose already scopes ordinary volumes, networks and services.
func checkInstallation(m compose.Model, ws string) error {
	for _, grant := range access.Grants(m, ws) {
		switch grant.Kind {
		case access.HostFolder, access.Engine, access.HostNamespace, access.Privileged, access.Device:
			return fmt.Errorf("a separate installation cannot isolate this recipe: %s", grant)
		}
	}
	for name, svc := range m.Services {
		for _, p := range svc.Ports {
			if p.Published != "" && p.Published != "0" {
				return fmt.Errorf("a separate installation cannot reuse the fixed host port %s of %s", p.Published, name)
			}
		}
	}
	return nil
}

// Claim images UGT created, including previously installed images used by
// this copy. An image already present outside UGT stays outside its ledger.
func (a *App) claimImages(cfg compose.Model, created []string, target string) error {
	live, err := a.live()
	if err != nil {
		return err
	}
	owned, claimed := map[string]bool{}, map[string]bool{}
	for _, e := range live {
		if e.Kind == ledger.Image {
			owned[e.Name] = true
			if e.Recipe == target {
				claimed[e.Name] = true
			}
		}
	}
	for _, s := range cfg.Services {
		if s.Builds() && !claimed[s.Image] && (owned[s.Image] || slices.Contains(created, s.Image)) {
			if err := a.record(ledger.Entry{Kind: ledger.Image, Name: s.Image, Recipe: target}); err != nil {
				return err
			}
			claimed[s.Image] = true
		}
	}
	return nil
}
