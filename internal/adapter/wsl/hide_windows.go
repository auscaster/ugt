//go:build windows

package wsl

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: the program gets no console window.
const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
