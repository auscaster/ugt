//go:build darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/auscaster/ugt/internal/adapter/host"
	"github.com/auscaster/ugt/internal/adapter/vm"
	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/workspace"
	"github.com/auscaster/ugt/layer"
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

func machineConfig(layout workspace.Layout) (vm.Config, error) {
	self, err := os.Executable()
	if err != nil {
		return vm.Config{}, err
	}
	return vm.Config{
		Machine:    layer.Machine(),
		Revision:   layer.Revision(),
		Init:       layer.MachineInit,
		Engine:     layer.EngineCommand,
		Poweroff:   layer.MachinePoweroff,
		Dir:        filepath.Join(layout.State, "vm"),
		Workspaces: filepath.Join(layout.State, "workspaces"),
		Serve:      []string{self, vm.ServeFlag},
	}, nil
}

// serveMachine runs UGT's machine when ugt was started as its background
// process, and tells the ugt that started it on file 3 when it is ready.
func serveMachine(args []string) (int, bool) {
	if len(args) != 1 || args[0] != vm.ServeFlag {
		return 0, false
	}
	home, err := os.UserHomeDir()
	if err == nil {
		var cfg vm.Config
		if cfg, err = machineConfig(workspace.DefaultLayout(runtime.GOOS, home, os.Getenv)); err == nil {
			err = vm.Serve(cfg, os.NewFile(3, "ready"))
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ugt: %v\n", err)
		return 1, true
	}
	return 0, true
}
