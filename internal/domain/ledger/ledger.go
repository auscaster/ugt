// Package ledger records everything UGT creates, so stop, remove, delete-data
// and uninstall act on exactly that and nothing else.
package ledger

import "time"

// Kind of thing created.
type Kind string

const (
	Image      Kind = "image"      // an image UGT built or pulled for a recipe
	Project    Kind = "project"    // a Compose project: its containers and networks
	Volume     Kind = "volume"     // a named volume holding a recipe's data
	Workspace  Kind = "workspace"  // a recipe's workspace folder
	Runtime    Kind = "runtime"    // UGT's own engine: the WSL distro or the VM
	Connection Kind = "connection" // an AI app UGT connected itself to
	Secret     Kind = "secret"     // a secret setting in the system's secret store, as recipe/NAME
	Command    Kind = "command"    // a command a recipe provides, put on the user's PATH, by its name
	MCPServer  Kind = "mcp-server" // a recipe's MCP server, served among UGT's own tools, as recipe/service
	Handler    Kind = "handler"    // the ugt:// link handler UGT registered with the system
)

// Entry is one thing UGT created.
type Entry struct {
	Kind    Kind      `json:"kind"`
	Name    string    `json:"name"`             // image reference, project name, path
	Recipe  string    `json:"recipe,omitempty"` // owner/repo/task, when it belongs to one
	Created time.Time `json:"created"`
	Removed time.Time `json:"removed,omitzero"`
}

// Live returns the entries not yet removed, latest record per kind and name.
func Live(entries []Entry) []Entry {
	type key struct {
		kind Kind
		name string
	}
	last := map[key]Entry{}
	var order []key
	for _, e := range entries {
		k := key{e.Kind, e.Name}
		if _, ok := last[k]; !ok {
			order = append(order, k)
		}
		last[k] = e
	}
	var live []Entry
	for _, k := range order {
		if e := last[k]; e.Removed.IsZero() {
			live = append(live, e)
		}
	}
	return live
}
