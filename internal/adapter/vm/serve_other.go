//go:build (darwin && !cgo) || linux

package vm

import (
	"errors"
	"io"
)

// Serve runs the machine only in a macOS build made with cgo, which Apple's
// Virtualization framework needs.
func Serve(_ Config, ready io.WriteCloser) error {
	return tell(ready, errors.New("this ugt was built without UGT's machine: macOS builds need cgo"))
}

// tell says why the machine could not start to the ugt waiting for it.
func tell(ready io.WriteCloser, err error) error {
	_, _ = io.WriteString(ready, err.Error()+"\n")
	_ = ready.Close()
	return err
}
