// Package layer is UGT's Linux layer contract: the distro it lives in, the
// Alpine Linux branch it is built on, how its engine starts, and the script
// that provisions it. ugt setup applies it to the WSL distro on Windows; CI
// will apply the same script to build a prebuilt layer.
package layer

import (
	_ "embed"
	"strings"
)

const (
	// Distro is the WSL distro UGT runs its engine in.
	Distro = "UGT"
	// AlpineBranch is the Alpine Linux release branch the layer is built on.
	AlpineBranch = "v3.24"
	// EngineCommand starts the engine if it is not running, and waits for it.
	EngineCommand = "/usr/local/bin/ugt-engine"
	// Workspaces is where recipe workspaces live in the layer.
	Workspaces = "/var/lib/ugt/workspaces"
)

//go:embed provision.sh
var provision string

// Provision is the script that provisions the layer, run as root with sh.
// Line endings are LF whatever the checkout did to them.
func Provision() string { return strings.ReplaceAll(provision, "\r\n", "\n") }
