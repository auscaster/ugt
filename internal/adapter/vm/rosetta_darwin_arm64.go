//go:build cgo

package vm

import vz "github.com/Code-Hex/vz/v4"

// rosetta is Apple's Rosetta for Linux, shared with the machine when this Mac
// has it, so images built only for amd64 run too. UGT never installs it:
// installing it asks the user.
func rosetta() (vz.DirectorySharingDeviceConfiguration, error) {
	if vz.LinuxRosettaDirectoryShareAvailability() != vz.LinuxRosettaAvailabilityInstalled {
		return nil, nil
	}
	share, err := vz.NewLinuxRosettaDirectoryShare()
	if err != nil {
		return nil, err
	}
	device, err := vz.NewVirtioFileSystemDeviceConfiguration(rosettaTag)
	if err != nil {
		return nil, err
	}
	device.SetDirectoryShare(share)
	return device, nil
}
