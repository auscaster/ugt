package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/auscaster/ugt/internal/domain/ledger"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

// UninstallResult is what uninstall did, or would do.
type UninstallResult struct {
	Applied bool     `json:"applied"`
	Steps   []string `json:"steps"`
	Kept    []string `json:"kept"`
	Failed  []string `json:"failed,omitempty"`
}

// stateFiles are what UGT keeps in its state folder, apart from the recipes
// the user made.
var stateFiles = []string{"ledger.jsonl", "jobs", "reports", "settings.json", "config.json", "logs", "workspaces", "wsl"}

// Uninstall removes everything UGT added to this machine, as the ledger
// records it: its connections to AI apps; each recipe's containers, images,
// workspaces and saved data; the secrets and settings it stored; what setup
// created (on Windows, the WSL distro with everything in it); and its state.
// The user's results in the UGT folder and the recipes they made stay. With
// apply false it only says what it would do. A step that fails does not stop
// the others; each failure is reported.
func (a *App) Uninstall(ctx context.Context, apply bool) (UninstallResult, error) {
	live, err := a.live()
	if err != nil {
		return UninstallResult{}, err
	}
	type step struct {
		what string
		do   func() error
	}
	var steps []step
	add := func(what string, do func() error) { steps = append(steps, step{what, do}) }

	var created []string
	recipes := map[string]bool{}
	for _, e := range live {
		switch {
		case e.Kind == ledger.Connection:
			name := e.Name
			add("disconnect UGT from "+name, func() error {
				_, err := a.Agents.Disconnect(ctx, name)
				return err
			})
		case e.Kind == ledger.Runtime:
			created = append(created, e.Name)
		case e.Recipe != "":
			recipes[e.Recipe] = true
		}
	}
	ids := make([]string, 0, len(recipes))
	for id := range recipes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(created) > 0 {
		// the runtime UGT created holds every container, image and volume
		add(fmt.Sprintf("remove %s, with every recipe's containers, images and saved data in it", strings.Join(created, ", ")),
			func() error { return a.Runtime.Teardown(ctx) })
	} else {
		for _, id := range ids {
			add("remove the containers, images, workspaces and saved data of "+id, func() error {
				if _, err := a.Remove(ctx, id); err != nil && !strings.Contains(err.Error(), "holds nothing") {
					return err
				}
				if _, err := a.DeleteData(ctx, id); err != nil && !strings.Contains(err.Error(), "holds nothing") {
					return err
				}
				return nil
			})
		}
	}
	if secrets := a.storedSecrets(ctx); len(secrets) > 0 {
		add(fmt.Sprintf("delete the %d secrets UGT stored in the system's secret store", len(secrets)), func() error {
			var errs []error
			for _, s := range secrets {
				errs = append(errs, a.Secrets.Delete(ctx, s.recipe, s.name))
			}
			return errors.Join(errs...)
		})
	}
	var present []string
	for _, name := range stateFiles {
		p := filepath.Join(a.Layout.State, name)
		if ok, _ := a.Disk.Exists(p); ok {
			present = append(present, p)
		}
	}
	if len(present) > 0 {
		add("delete UGT's records in "+a.Layout.State+": its ledger, jobs, reports, settings and workspaces", func() error {
			var errs []error
			for _, p := range present {
				errs = append(errs, a.Disk.RemoveAll(p))
			}
			return errors.Join(errs...)
		})
	}

	res := UninstallResult{Applied: apply, Steps: []string{}}
	for _, s := range steps {
		res.Steps = append(res.Steps, s.what)
	}
	if ok, _ := a.Disk.Exists(a.Layout.Home); ok {
		res.Kept = append(res.Kept, "your files and results in "+a.Layout.Home)
	}
	if ok, _ := a.Disk.Exists(a.Layout.Recipes()); ok {
		res.Kept = append(res.Kept, "the recipes you made, in "+a.Layout.Recipes())
	}
	res.Kept = append(res.Kept, "the ugt program itself: remove it the way you installed it")
	if !apply {
		return res, nil
	}
	for _, s := range steps {
		if err := s.do(); err != nil {
			res.Failed = append(res.Failed, s.what+": "+err.Error())
		}
	}
	if len(res.Failed) > 0 {
		return res, fmt.Errorf("uninstall left %d steps undone", len(res.Failed))
	}
	return res, nil
}

type storedSecret struct{ recipe, name string }

// storedSecrets finds the secrets UGT holds, for every recipe it knows.
func (a *App) storedSecrets(ctx context.Context) []storedSecret {
	if a.Secrets == nil {
		return nil
	}
	ix, err := a.index(ctx)
	if err != nil {
		return nil
	}
	var out []storedSecret
	for _, e := range ix {
		if e.Problem != "" {
			continue
		}
		id, err := recipe.ParseID(e.Recipe)
		if err != nil {
			continue
		}
		_, src, err := a.resolve(ctx, id.Owner, id.Repo, id.Task)
		if err != nil {
			continue
		}
		f, err := recipe.Load(src)
		if err != nil {
			continue
		}
		for _, s := range f.Recipe.Settings {
			if s.Kind != "secret" {
				continue
			}
			if _, ok, err := a.Secrets.Get(ctx, e.Recipe, s.Name); err == nil && ok {
				out = append(out, storedSecret{e.Recipe, s.Name})
			}
		}
	}
	return out
}
