package app

import (
	"sync"

	"github.com/usegeekthing/ugt/internal/domain/ledger"
	"github.com/usegeekthing/ugt/internal/domain/workspace"
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
	Secrets    Secrets
	Values     Values
	Consents   Consents
	Prompt     Prompt     // nil when nothing can ask the user
	Reports    RunReports // nil when the published catalogue takes no reports
	Agents     AgentHosts
	Self       MCPServer // how AI apps start this ugt's MCP server
	Launchers  Launchers // put the commands recipes provide on the user's PATH
	FreePort   FreePort  // finds the host ports UGT publishes what recipes provide on
	Listening  Listening // whether one of them answers yet
	Releases   Releases  // nil when this ugt has no releases to update from
	Browser    Browser   // nil where nothing can be opened for the user
	Links      LinkHandler
	Jobs       Jobs
	Ledger     Ledger
	Layout     workspace.Layout // Workspaces is the runtime's
	Now        Clock
	Version    string // ugt's version, as its release set it

	jobsOnce sync.Once
	jobs     *jobs
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
