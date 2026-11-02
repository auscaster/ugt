//go:build !darwin

package main

import (
	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// machineRuntime is UGT's own Linux machine, which only Macs run.
func machineRuntime(workspace.Layout) (app.Runtime, error) { return nil, nil }

// serveMachine runs UGT's machine, which only Macs run.
func serveMachine([]string) (int, bool) { return 0, false }
