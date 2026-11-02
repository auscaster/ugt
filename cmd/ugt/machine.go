//go:build darwin || linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/auscaster/ugt/internal/adapter/vm"
	"github.com/auscaster/ugt/internal/domain/workspace"
	"github.com/auscaster/ugt/layer"
)

func machineConfig(layout workspace.Layout) (vm.Config, error) {
	self, err := os.Executable()
	if err != nil {
		return vm.Config{}, err
	}
	cfg := vm.Config{
		Machine:    layer.Machine(),
		Revision:   layer.Revision(),
		Init:       layer.MachineInit,
		Engine:     layer.EngineCommand,
		Poweroff:   layer.MachinePoweroff,
		Mount9p:    layer.MachineMount9p,
		Dir:        filepath.Join(layout.State, "vm"),
		Workspaces: filepath.Join(layout.State, "workspaces"),
		Serve:      []string{self, vm.ServeFlag},
	}
	if runtime.GOOS == "linux" {
		cfg.QEMU = layer.QEMU()
	}
	return cfg, nil
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
