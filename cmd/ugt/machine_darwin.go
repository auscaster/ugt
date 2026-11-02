//go:build darwin

package main

import (
	"os"
	"runtime"

	"github.com/auscaster/ugt/internal/adapter/host"
	"github.com/auscaster/ugt/internal/adapter/vm"
	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// machineRuntime is UGT's own Linux machine, on an Apple silicon Mac, unless
// UGT_RUNTIME=host uses an engine the user runs, such as Docker Desktop, as on
// Windows. Intel Macs use the user's engine until the machine is built for them.
func machineRuntime(layout workspace.Layout) (app.Runtime, error) {
	if runtime.GOARCH != "arm64" || os.Getenv("UGT_RUNTIME") == "host" {
		return nil, nil
	}
	cfg, err := machineConfig(layout)
	if err != nil {
		return nil, err
	}
	return vm.New(cfg, host.Files{}, layers()), nil
}
