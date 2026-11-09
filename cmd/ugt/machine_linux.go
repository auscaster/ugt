//go:build linux

package main

import (
	"os"
	"os/exec"

	"github.com/usegeekthing/ugt/internal/adapter/fsstate"
	"github.com/usegeekthing/ugt/internal/adapter/host"
	"github.com/usegeekthing/ugt/internal/adapter/vm"
	"github.com/usegeekthing/ugt/internal/app"
	"github.com/usegeekthing/ugt/internal/domain/workspace"
)

// machineRuntime is UGT's own Linux machine under QEMU with KVM, unless
// UGT_RUNTIME=host uses an engine the user runs. Where this user cannot use
// KVM, an engine already here is used; with neither, the machine says the one
// admin step KVM needs. An install that runs its recipes in the engine here
// keeps it (usesItsEngine).
func machineRuntime(layout workspace.Layout) (app.Runtime, error) {
	if os.Getenv("UGT_RUNTIME") == "host" {
		return nil, nil
	}
	if _, err := exec.LookPath("docker"); err == nil {
		if vm.KVM() != nil {
			return nil, nil
		}
		if entries, err := fsstate.NewLedger(layout.Ledger()).Entries(); err == nil && usesItsEngine(entries) {
			return nil, nil
		}
	}
	cfg, err := machineConfig(layout)
	if err != nil {
		return nil, err
	}
	return vm.New(cfg, host.Files{}, layers()), nil
}
