//go:build cgo

package vm

import vz "github.com/Code-Hex/vz/v4"

// rosetta is for Apple silicon only.
func rosetta() (vz.DirectorySharingDeviceConfiguration, error) { return nil, nil }
