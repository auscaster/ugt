package main

import (
	"os"
	"os/exec"
	"syscall"
)

// detached relaunches ugt open without a console window and reports whether
// it did, so the caller exits. Windows gives a console program a window of
// its own when the link handler starts it, and the page would keep it open
// for as long as it runs.
func detached(args []string) bool {
	if len(args) != 2 || args[0] != "open" || os.Getenv("UGT_DETACHED") != "" {
		return false
	}
	if w, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow").Call(); w == 0 {
		return false
	}
	self, err := os.Executable()
	if err != nil {
		return false
	}
	cmd := exec.Command(self, args...)
	cmd.Env = append(os.Environ(), "UGT_DETACHED=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000 | 0x00000008} // CREATE_NO_WINDOW | DETACHED_PROCESS
	return cmd.Start() == nil
}
