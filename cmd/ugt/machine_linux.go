//go:build linux

package main

import (
	"os"
	"os/exec"

	"github.com/auscaster/ugt/internal/adapter/host"
	"github.com/auscaster/ugt/internal/adapter/vm"
	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// machineRuntime is UGT's own Linux machine under QEMU with KVM, unless
// UGT_RUNTIME=host uses an engine the user runs. Where this user cannot use
// KVM, an engine already here is used; with neither, the machine says the one
// admin step KVM needs.
func machineRuntime(layout workspace.Layout) (app.Runtime, error) {
	if os.Getenv("UGT_RUNTIME") == "host" {
		return nil, nil
	}
	if vm.KVM() != nil {
		if _, err := exec.LookPath("docker"); err == nil {
			return nil, nil
		}
	}
	cfg, err := machineConfig(layout)
	if err != nil {
		return nil, err
	}
	return vm.New(cfg, host.Files{}, layers()), nil
}
