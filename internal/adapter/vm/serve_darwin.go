//go:build darwin && cgo

package vm

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"

	vz "github.com/Code-Hex/vz/v4"
	"github.com/containers/gvisor-tap-vsock/pkg/types"
	"github.com/containers/gvisor-tap-vsock/pkg/virtualnetwork"
	"golang.org/x/sys/unix"
)

// vzMachine runs the machine on Apple's Virtualization framework.
type vzMachine struct {
	cfg Config
	p   paths

	vm      *vz.VirtualMachine
	folders []*vz.VirtioFileSystemDevice // the devices commands' folders are shared on, one each
	rosetta bool                         // Rosetta is shared, for amd64 programs
	vsock   *vz.VirtioSocketDevice

	mu   sync.Mutex
	used map[int]bool // folder devices sharing a command's folder
}

func newHypervisor(cfg Config, p paths) (hypervisor, error) {
	return &vzMachine{cfg: cfg, p: p, used: map[int]bool{}}, nil
}

// folderDevices is how many commands' folders the machine can share at once.
const folderDevices = 8

// rosettaTag is Rosetta's device, mounted in the machine at /mnt/rosetta.
const rosettaTag = "rosetta"

// rosettaFormat registers Rosetta in the machine for amd64 Linux programs,
// as Apple documents it: the ELF header of an x86-64 executable, and its mask.
const rosettaFormat = `:rosetta:M::\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x02\x00\x3e\x00:` +
	`\xff\xff\xff\xff\xff\xfe\xfe\x00\xff\xff\xff\xff\xff\xff\xff\xff\xfe\xff\xff\xff:/mnt/rosetta/rosetta:OCF`

// boot starts the machine: the layer's root read-only under a memory overlay,
// the data disk, the network in this process, vsock, and the shared folders.
func (h *vzMachine) boot(stopped func()) error {
	p := h.p
	loader, err := vz.NewLinuxBootLoader(p.file(kernelFile), vz.WithInitrd(p.file(initramfsFile)),
		vz.WithCommandLine("console=hvc0 root=/dev/vda rootfstype=squashfs overlaytmpfs modules=squashfs,virtio_blk init="+h.cfg.Init+" quiet"))
	if err != nil {
		return err
	}
	cpus, memory := resources()
	cfg, err := vz.NewVirtualMachineConfiguration(loader, cpus, memory)
	if err != nil {
		return err
	}
	_ = os.Remove(p.console())
	console, err := vz.NewFileSerialPortAttachment(p.console(), false)
	if err != nil {
		return err
	}
	serial, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(console)
	if err != nil {
		return err
	}
	cfg.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{serial})
	var disks []vz.StorageDeviceConfiguration
	for _, d := range []struct {
		path     string
		readOnly bool
	}{{p.file(rootFile), true}, {p.disk(), false}} {
		attachment, err := vz.NewDiskImageStorageDeviceAttachment(d.path, d.readOnly)
		if err != nil {
			return err
		}
		disk, err := vz.NewVirtioBlockDeviceConfiguration(attachment)
		if err != nil {
			return err
		}
		disks = append(disks, disk)
	}
	cfg.SetStorageDevicesVirtualMachineConfiguration(disks)
	nic, err := network()
	if err != nil {
		return err
	}
	cfg.SetNetworkDevicesVirtualMachineConfiguration([]*vz.VirtioNetworkDeviceConfiguration{nic})
	entropy, err := vz.NewVirtioEntropyDeviceConfiguration()
	if err != nil {
		return err
	}
	cfg.SetEntropyDevicesVirtualMachineConfiguration([]*vz.VirtioEntropyDeviceConfiguration{entropy})
	socket, err := vz.NewVirtioSocketDeviceConfiguration()
	if err != nil {
		return err
	}
	cfg.SetSocketDevicesVirtualMachineConfiguration([]vz.SocketDeviceConfiguration{socket})
	if err := os.MkdirAll(h.cfg.Workspaces, 0o755); err != nil {
		return err
	}
	devices, err := h.directoryDevices()
	if err != nil {
		return err
	}
	cfg.SetDirectorySharingDevicesVirtualMachineConfiguration(devices)
	if ok, err := cfg.Validate(); !ok {
		return entitled(fmt.Errorf("UGT's machine is not one this Mac can run: %w", err))
	}
	h.vm, err = vz.NewVirtualMachine(cfg)
	if err != nil {
		return entitled(err)
	}
	go func() {
		for s := range h.vm.StateChangedNotify() {
			if s == vz.VirtualMachineStateStopped || s == vz.VirtualMachineStateError {
				stopped()
				return
			}
		}
	}()
	if err := h.vm.Start(); err != nil {
		return entitled(fmt.Errorf("start UGT's machine: %w", err))
	}
	h.vsock = h.vm.SocketDevices()[0]
	for _, d := range h.vm.DirectorySharingDevices()[1 : 1+folderDevices] {
		h.folders = append(h.folders, d.(*vz.VirtioFileSystemDevice))
	}
	return nil
}

// entitled names the fix when this ugt may not run virtual machines.
func entitled(err error) error {
	if strings.Contains(err.Error(), "entitlement") {
		return fmt.Errorf("%w: this ugt is not signed to run virtual machines; install UGT with its install script, which signs it", err)
	}
	return err
}

// resources gives the machine every CPU and half the Mac's memory up to 8 GB,
// as Docker Desktop does. The machine takes memory as it uses it, but Apple's
// framework never takes back what it has used, page cache included, so it is
// capped where the largest recipes fit.
func resources() (uint, uint64) {
	cpus := uint(runtime.NumCPU())
	cpus = max(vz.VirtualMachineConfigurationMinimumAllowedCPUCount(), min(cpus, vz.VirtualMachineConfigurationMaximumAllowedCPUCount()))
	memory := uint64(4 << 30)
	if total, err := unix.SysctlUint64("hw.memsize"); err == nil {
		memory = max(min(total/2, 8<<30), 2<<30)
	}
	memory = min(memory, vz.VirtualMachineConfigurationMaximumAllowedMemorySize())
	return cpus, memory
}

// network is the machine's network card, served by gvisor-tap-vsock in this
// process: every connection the machine makes is one this process makes, so
// it goes wherever the Mac's own do, through a VPN too, with no admin rights.
func network() (*vz.VirtioNetworkDeviceConfiguration, error) {
	vn, err := virtualnetwork.New(&types.Configuration{MTU: 1500, Subnet: subnet, GatewayIP: gateway,
		GatewayMacAddress: gatewayMAC, DHCPStaticLeases: map[string]string{machineIP: machineMAC}})
	if err != nil {
		return nil, err
	}
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return nil, err
	}
	for _, fd := range fds {
		_ = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_SNDBUF, 1<<20)
		_ = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, 4<<20)
	}
	conn, err := net.FileConn(os.NewFile(uintptr(fds[1]), "network"))
	if err != nil {
		return nil, err
	}
	go func() {
		if err := vn.AcceptVfkit(context.Background(), conn); err != nil {
			fmt.Fprintf(os.Stderr, "ugt: the machine's network ended: %v\n", err)
		}
	}()
	attachment, err := vz.NewFileHandleNetworkDeviceAttachment(os.NewFile(uintptr(fds[0]), "machine"))
	if err != nil {
		return nil, err
	}
	nic, err := vz.NewVirtioNetworkDeviceConfiguration(attachment)
	if err != nil {
		return nil, err
	}
	mac, err := net.ParseMAC(machineMAC)
	if err != nil {
		return nil, err
	}
	address, err := vz.NewMACAddress(mac)
	if err != nil {
		return nil, err
	}
	nic.SetMACAddress(address)
	return nic, nil
}

// directoryDevices are the machine's shared folders. UGT's own, read-only,
// and the workspaces are under /mnt/ugt on one device, which never changes:
// changing a device's folders breaks every mount of them, a running
// container's too. Each command's folder gets a device of its own, empty
// until then.
func (h *vzMachine) directoryDevices() ([]vz.DirectorySharingDeviceConfiguration, error) {
	own, err := vz.NewSharedDirectory(h.p.share(), true)
	if err != nil {
		return nil, err
	}
	workspaces, err := vz.NewSharedDirectory(h.cfg.Workspaces, false)
	if err != nil {
		return nil, err
	}
	fixed, err := vz.NewMultipleDirectoryShare(map[string]*vz.SharedDirectory{"ugt": own, "workspaces": workspaces})
	if err != nil {
		return nil, err
	}
	device, err := vz.NewVirtioFileSystemDeviceConfiguration("ugt")
	if err != nil {
		return nil, err
	}
	device.SetDirectoryShare(fixed)
	devices := []vz.DirectorySharingDeviceConfiguration{device}
	for i := range folderDevices {
		empty, err := h.emptyShare()
		if err != nil {
			return nil, err
		}
		device, err := vz.NewVirtioFileSystemDeviceConfiguration(folderTag(i))
		if err != nil {
			return nil, err
		}
		device.SetDirectoryShare(empty)
		devices = append(devices, device)
	}
	r, err := rosetta()
	if err != nil {
		return nil, err
	}
	if r != nil {
		devices = append(devices, r)
		h.rosetta = true
	}
	return devices, nil
}

func folderTag(device int) string { return "folder" + strconv.Itoa(device+1) }

// emptyShare is what a command's device shares while no command uses it: an
// empty folder, read-only.
func (h *vzMachine) emptyShare() (*vz.SingleDirectoryShare, error) {
	dir := filepath.Join(h.p.dir, "empty")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	shared, err := vz.NewSharedDirectory(dir, true)
	if err != nil {
		return nil, err
	}
	return vz.NewSingleDirectoryShare(shared)
}

func (h *vzMachine) dial() (net.Conn, error) { return h.vsock.Connect(22) }

// attach shares a command's folder on a device of its own.
func (h *vzMachine) attach(dir string) (func(string) string, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	device := -1
	for i := range h.folders {
		if !h.used[i] {
			device = i
			break
		}
	}
	if device < 0 {
		return nil, nil, fmt.Errorf("UGT's machine is sharing %d commands' folders already: try again once one has finished", folderDevices)
	}
	shared, err := vz.NewSharedDirectory(dir, false)
	if err != nil {
		return nil, nil, err
	}
	single, err := vz.NewSingleDirectoryShare(shared)
	if err != nil {
		return nil, nil, err
	}
	if err := h.folders[device].SetDirectoryShare(single); err != nil {
		return nil, nil, err
	}
	h.used[device] = true
	mount := func(target string) string { return "mount -t virtiofs " + folderTag(device) + " " + target }
	return mount, func() { h.empty(device) }, nil
}

// empty takes a command's folder off its device, once it is unmounted.
func (h *vzMachine) empty(device int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	share, err := h.emptyShare()
	if err == nil {
		err = h.folders[device].SetDirectoryShare(share)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ugt: empty the machine's folder device %d: %v\n", device+1, err)
	}
	delete(h.used, device)
}

// ready registers Rosetta in the machine, when the Mac has it.
func (h *vzMachine) ready(run func(string) (string, error)) {
	if !h.rosetta {
		return
	}
	if _, err := run("modprobe binfmt_misc; mkdir -p /mnt/rosetta && mount -t virtiofs " + rosettaTag + " /mnt/rosetta" +
		" && { mountpoint -q /proc/sys/fs/binfmt_misc || mount -t binfmt_misc binfmt_misc /proc/sys/fs/binfmt_misc; }" +
		" && printf '%s' " + quote(rosettaFormat) + " > /proc/sys/fs/binfmt_misc/register"); err != nil {
		fmt.Fprintf(os.Stderr, "ugt: images built only for amd64 will not run: Rosetta did not start in the machine: %v\n", err)
	}
}

func (h *vzMachine) kill() {
	if h.vm != nil {
		_ = h.vm.Stop()
	}
}
