package app

import (
	"context"

	"github.com/auscaster/ugt/internal/domain/ledger"
)

// Connection is what connecting or disconnecting one AI app did.
type Connection struct {
	App  string `json:"app"`
	Done string `json:"done"`
}

// Connect connects UGT's MCP server to the named AI apps, or to every one
// installed here that is not connected yet or has UGT twice. Each is recorded
// in the ledger, so uninstall can disconnect it. With every app connected
// once already, through UGT or UGT's plugin in it, each says how, and that is
// no failure. Finding none is also valid: UGT works from the browser without
// an AI app, and connecting one can wait until the user installs it.
func (a *App) Connect(ctx context.Context, names []string) ([]Connection, error) {
	found, err := a.Agents.Find(ctx)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		for _, h := range found {
			if !h.Connected || h.Twice {
				names = append(names, h.Name)
			}
		}
		if len(names) == 0 {
			out := make([]Connection, 0, len(found))
			for _, h := range found {
				out = append(out, Connection{App: h.Name, Done: "already connected, through " + h.Where})
			}
			return out, nil
		}
	}
	var out []Connection
	for _, name := range names {
		done, err := a.Agents.Connect(ctx, name, a.Self)
		if err != nil {
			return out, err
		}
		_ = a.record(ledger.Entry{Kind: ledger.Connection, Name: name})
		out = append(out, Connection{App: name, Done: done})
	}
	return out, nil
}

// Disconnect removes UGT's MCP server from the named AI apps, or from every
// one it is connected to.
func (a *App) Disconnect(ctx context.Context, names []string) ([]Connection, error) {
	if len(names) == 0 {
		found, err := a.Agents.Find(ctx)
		if err != nil {
			return nil, err
		}
		for _, h := range found {
			if h.Connected {
				names = append(names, h.Name)
			}
		}
	}
	var out []Connection
	for _, name := range names {
		done, err := a.Agents.Disconnect(ctx, name)
		if err != nil {
			return out, err
		}
		_ = a.record(ledger.Entry{Kind: ledger.Connection, Name: name, Removed: a.Now()})
		out = append(out, Connection{App: name, Done: done})
	}
	return out, nil
}
