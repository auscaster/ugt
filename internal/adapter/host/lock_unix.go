//go:build !windows

package host

import (
	"errors"
	"os"
	"syscall"
)

func lock(f *os.File, wait bool) (bool, error) {
	flags := syscall.LOCK_EX
	if !wait {
		flags |= syscall.LOCK_NB
	}
	err := syscall.Flock(int(f.Fd()), flags)
	if !wait && errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}
