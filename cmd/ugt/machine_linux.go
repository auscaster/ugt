//go:build linux

package main

import (
	"os"
	"os/exec"
	"runtime"

	"github.com/auscaster/ugt/internal/adapter/host"
	"github.com/auscaster/ugt/internal/adapter/vm"
	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// machineRuntime is UGT's own Linux machine under QEMU with KVM, on amd64,
// unless UGT_RUNTIME=host uses an engine the user runs. Where this user cannot
// use KVM, an engine already here is used; with neither, the machine says the
// one admin step KVM needs. arm64 uses the user's engine until the machine is
// proved there.
func machineRuntime(layout workspace.Layout) (app.Runtime, error) {
	if runtime.GOARCH != "amd64" || os.Getenv("UGT_RUNTIME") == "host" {
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
