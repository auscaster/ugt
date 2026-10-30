package app

import (
	"github.com/auscaster/ugt/internal/domain/ledger"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// App runs UGT's use cases against the ports it is given.
type App struct {
	Runtime Runtime
	Engine  Engine
	Disk    Disk
	GitHub  GitHub
	// Catalogues are the sources of recipes, in order of precedence: the
	// local catalogue first, so a recipe made here wins.
	Catalogues []Catalogue
	Jobs       Jobs
	Ledger     Ledger
	Layout     workspace.Layout // Workspaces is the runtime's
	Now        Clock
}

// record notes something UGT created. A ledger failure must not hide the
// result of the work itself, so it is returned for the caller to report.
func (a *App) record(e ledger.Entry) error {
	if a.Ledger == nil {
		return nil
	}
	if e.Created.IsZero() && e.Removed.IsZero() {
		e.Created = a.Now()
	}
	return a.Ledger.Record(e)
}
