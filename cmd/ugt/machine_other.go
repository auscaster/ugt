//go:build !darwin && !linux

package main

import (
	"github.com/usegeekthing/ugt/internal/app"
	"github.com/usegeekthing/ugt/internal/domain/workspace"
)

// machineRuntime is UGT's own Linux machine, which Windows does not run: it
// has UGT's WSL distro.
func machineRuntime(workspace.Layout) (app.Runtime, error) { return nil, nil }

// serveMachine runs UGT's machine, which Windows does not run.
func serveMachine([]string) (int, bool) { return 0, false }
