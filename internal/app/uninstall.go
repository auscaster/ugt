package app

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/auscaster/ugt/internal/domain/ledger"
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
var stateFiles = []string{"ledger.jsonl", "jobs", "reports", "settings.json", "consents.json", "config.json", "logs", "workspaces", "wsl", "cache"}

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
	var secrets []ledger.Entry
	recipes := map[string]bool{}
	for _, e := range live {
		switch {
		case e.Kind == ledger.Secret:
			secrets = append(secrets, e)
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
	if len(secrets) > 0 && a.Secrets != nil {
		add(fmt.Sprintf("delete the %d secrets UGT stored in the system's secret store", len(secrets)), func() error {
			var errs []error
			for _, s := range secrets {
				recipeID, name := path.Split(s.Name)
				errs = append(errs, a.Secrets.Delete(ctx, strings.TrimSuffix(recipeID, "/"), name))
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
