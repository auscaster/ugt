//go:build darwin && cgo

package vm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	vz "github.com/Code-Hex/vz/v4"
	"github.com/containers/gvisor-tap-vsock/pkg/types"
	"github.com/containers/gvisor-tap-vsock/pkg/virtualnetwork"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

// idle is how long the machine runs with no one using it, and no container
// running, before it powers off, as Docker Desktop's Resource Saver does.
const idle = 5 * time.Minute

// The network UGT runs for the machine, in this process: the machine is .2 and
// the Mac .1, as the machine's init sets it.
const (
	subnet     = "192.168.127.0/24"
	gateway    = "192.168.127.1"
	gatewayMAC = "5a:94:ef:e4:0c:dd"
	machineIP  = "192.168.127.2"
	machineMAC = "5a:94:ef:e4:0c:ee"
)

// Serve runs UGT's machine until it stops: asked to, idle, or powered off from
// inside. It is the background process every ugt on the Mac reaches the
// machine through, on its socket. ready is told readyLine once the engine
// answers, or why the machine could not start.
func Serve(cfg Config, ready io.WriteCloser) error {
	m := &machine{cfg: cfg, p: paths{cfg.Dir, cfg.Revision}, shares: map[string]*share{},
		forwards: map[int]net.Listener{}, done: make(chan struct{}), stopped: make(chan struct{}), busy: time.Now()}
	// a machine powering off still holds the lock for a few seconds
	unlock, err := m.p.lock("machine.lock", false)
	for wait := time.Now().Add(time.Minute); err != nil && time.Now().Before(wait); {
		time.Sleep(200 * time.Millisecond)
		unlock, err = m.p.lock("machine.lock", false)
	}
	if err != nil {
		return tell(ready, errors.New("UGT's machine is running in another process already"))
	}
	defer unlock()
	if err := m.listen(); err != nil {
		return tell(ready, err)
	}
	started := time.Now()
	if err := m.boot(); err != nil {
		m.closeSocket()
		return tell(ready, err)
	}
	if err := m.prepare(); err != nil {
		m.shutdown()
		return tell(ready, err)
	}
	fmt.Fprintf(os.Stderr, "ugt: machine on layer %s ready in %.1fs\n", cfg.Revision, time.Since(started).Seconds())
	_ = tell(ready, nil)
	go m.accept(m.ln)
	go m.follow()
	return m.watch()
}

// tell says readyLine, or err, to the ugt waiting for the machine.
func tell(ready io.WriteCloser, err error) error {
	line := readyLine
	if err != nil {
		line = err.Error()
	}
	_, _ = fmt.Fprintln(ready, line)
	_ = ready.Close()
	return err
}

type machine struct {
	cfg Config
	p   paths

	vm      *vz.VirtualMachine
	folders []*vz.VirtioFileSystemDevice // the devices commands' folders are shared on, one each
	rosetta bool                         // Rosetta is shared, for amd64 programs
	vsock   *vz.VirtioSocketDevice
	ssh     *ssh.Client // this process's own connection, for its own commands
	ln      net.Listener

	mu       sync.Mutex
	closing  bool              // shutting down: no new socket or forward
	shares   map[string]*share // by folder on the Mac
	conns    int               // ugts connected now
	running  int               // containers running
	busy     time.Time         // when the machine was last in use
	forwards map[int]net.Listener

	once    sync.Once
	done    chan struct{} // closed once the machine has stopped
	stopped chan struct{} // closed when the machine stops, whoever stopped it
}

// share is a folder of the user's shared for the commands using it.
type share struct {
	device int // its device in folders
	uses   int
}

// folderDevices is how many commands' folders the machine can share at once.
const folderDevices = 8

// rosettaTag is Rosetta's device, mounted in the machine at /mnt/rosetta.
const rosettaTag = "rosetta"

// rosettaFormat registers Rosetta in the machine for amd64 Linux programs,
// as Apple documents it: the ELF header of an x86-64 executable, and its mask.
const rosettaFormat = `:rosetta:M::\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x02\x00\x3e\x00:` +
	`\xff\xff\xff\xff\xff\xfe\xfe\x00\xff\xff\xff\xff\xff\xff\xff\xff\xfe\xff\xff\xff:/mnt/rosetta/rosetta:OCF`

// listen takes the machine's socket, in a folder only this user can reach.
func (m *machine) listen() error {
	path := m.p.socket()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var st syscall.Stat_t
	if err := syscall.Lstat(dir, &st); err != nil {
		return err
	}
	if st.Uid != uint32(os.Getuid()) || st.Mode&0o077 != 0 || st.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		return fmt.Errorf("%s is not a folder only this user can reach: remove it", dir)
	}
	_ = os.Remove(path) // the machine.lock held means no other process serves it
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		_ = ln.Close()
		return errors.New("UGT's machine is stopping")
	}
	if m.ln != nil {
		_ = m.ln.Close()
	}
	m.ln = ln
	return nil
}

func (m *machine) closeSocket() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closing = true
	if m.ln != nil {
		_ = m.ln.Close()
	}
	_ = os.Remove(m.p.socket())
}

// boot starts the machine: the layer's root read-only under a memory overlay,
// the data disk, the network in this process, vsock, and the shared folders.
func (m *machine) boot() error {
	p := m.p
	loader, err := vz.NewLinuxBootLoader(p.file(kernelFile), vz.WithInitrd(p.file(initramfsFile)),
		vz.WithCommandLine("console=hvc0 root=/dev/vda rootfstype=squashfs overlaytmpfs modules=squashfs,virtio_blk init="+m.cfg.Init+" quiet"))
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
	if err := os.MkdirAll(m.cfg.Workspaces, 0o755); err != nil {
		return err
	}
	devices, err := m.directoryDevices()
	if err != nil {
		return err
	}
	cfg.SetDirectorySharingDevicesVirtualMachineConfiguration(devices)
	if ok, err := cfg.Validate(); !ok {
		return entitled(fmt.Errorf("UGT's machine is not one this Mac can run: %w", err))
	}
	m.vm, err = vz.NewVirtualMachine(cfg)
	if err != nil {
		return entitled(err)
	}
	go func() {
		for s := range m.vm.StateChangedNotify() {
			if s == vz.VirtualMachineStateStopped || s == vz.VirtualMachineStateError {
				close(m.stopped)
				return
			}
		}
	}()
	if err := m.vm.Start(); err != nil {
		return entitled(fmt.Errorf("start UGT's machine: %w", err))
	}
	m.vsock = m.vm.SocketDevices()[0]
	for _, d := range m.vm.DirectorySharingDevices()[1 : 1+folderDevices] {
		m.folders = append(m.folders, d.(*vz.VirtioFileSystemDevice))
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
func (m *machine) directoryDevices() ([]vz.DirectorySharingDeviceConfiguration, error) {
	own, err := vz.NewSharedDirectory(m.p.share(), true)
	if err != nil {
		return nil, err
	}
	workspaces, err := vz.NewSharedDirectory(m.cfg.Workspaces, false)
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
		empty, err := m.emptyShare()
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
		m.rosetta = true
	}
	return devices, nil
}

func folderTag(device int) string { return "folder" + strconv.Itoa(device+1) }

// emptyShare is what a command's device shares while no command uses it: an
// empty folder, read-only.
func (m *machine) emptyShare() (*vz.SingleDirectoryShare, error) {
	dir := filepath.Join(m.p.dir, "empty")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	shared, err := vz.NewSharedDirectory(dir, true)
	if err != nil {
		return nil, err
	}
	return vz.NewSingleDirectoryShare(shared)
}

// prepare waits for the machine's sshd, then sets its clock, puts the
// workspaces at their own path, and waits for its engine.
func (m *machine) prepare() error {
	config, err := m.p.clientConfig()
	if err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Minute)
	for m.ssh == nil {
		select {
		case <-m.stopped:
			return fmt.Errorf("UGT's machine stopped as it started: %s", lastLines(m.p.console(), 5))
		default:
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("UGT's machine did not answer within 2 minutes: %s", lastLines(m.p.console(), 5))
		}
		if conn, err := m.vsock.Connect(22); err == nil {
			if sc, chans, reqs, err := ssh.NewClientConn(conn, "ugt", config); err == nil {
				m.ssh = ssh.NewClient(sc, chans, reqs)
				break
			}
			_ = conn.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	m.setClock()
	w := quote(m.cfg.Workspaces)
	if _, err := m.run("mkdir -p " + w + " && mount --bind /mnt/ugt/workspaces " + w); err != nil {
		return fmt.Errorf("share the workspaces with UGT's machine: %w", err)
	}
	if m.rosetta {
		if _, err := m.run("modprobe binfmt_misc; mkdir -p /mnt/rosetta && mount -t virtiofs " + rosettaTag + " /mnt/rosetta" +
			" && { mountpoint -q /proc/sys/fs/binfmt_misc || mount -t binfmt_misc binfmt_misc /proc/sys/fs/binfmt_misc; }" +
			" && printf '%s' " + quote(rosettaFormat) + " > /proc/sys/fs/binfmt_misc/register"); err != nil {
			fmt.Fprintf(os.Stderr, "ugt: images built only for amd64 will not run: Rosetta did not start in the machine: %v\n", err)
		}
	}
	if _, err := m.run(quote(m.cfg.Engine)); err != nil {
		return fmt.Errorf("start the engine in UGT's machine: %w", err)
	}
	return nil
}

// run runs a shell line in the machine for this process.
func (m *machine) run(line string) (string, error) {
	s, err := m.ssh.NewSession()
	if err != nil {
		return "", err
	}
	defer s.Close()
	out, err := s.CombinedOutput(line)
	if err != nil {
		return string(out), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// setClock sets the machine's clock to the Mac's: at boot, and after the Mac
// sleeps, when the machine's clock stood still.
func (m *machine) setClock() {
	if _, err := m.run(fmt.Sprintf("date -u -s @%d >/dev/null", time.Now().Unix())); err != nil {
		fmt.Fprintf(os.Stderr, "ugt: set the machine's clock: %v\n", err)
	}
}

func (m *machine) accept(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go m.handle(conn)
	}
}

// handle answers one ugt's request. A connection for SSH is then carried to
// a new sshd in the machine until either side closes it.
func (m *machine) handle(conn net.Conn) {
	defer conn.Close()
	m.use(1)
	defer m.use(-1)
	in := bufio.NewReader(conn)
	line, err := in.ReadBytes('\n')
	if err != nil {
		return
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		_ = json.NewEncoder(conn).Encode(response{Error: "not a request: " + err.Error()})
		return
	}
	answer := func(err error) error {
		res := response{Revision: m.cfg.Revision}
		if err != nil {
			res.Error = err.Error()
		}
		return json.NewEncoder(conn).Encode(res)
	}
	switch req.Op {
	case opStatus:
		_ = answer(nil)
	case opSSH:
		guest, err := m.vsock.Connect(22)
		if err != nil {
			_ = answer(fmt.Errorf("reach the machine's sshd: %w", err))
			return
		}
		defer guest.Close()
		if answer(nil) == nil {
			pipe(buffered{conn, in}, guest)
		}
	case opFolder:
		_ = answer(m.share(req.Path))
	case opRelease:
		m.release(req.Path)
		_ = answer(nil)
	case opStop:
		m.shutdown()
		_ = answer(nil)
	default:
		_ = answer(fmt.Errorf("no such request: %q", req.Op))
	}
}

// pipe carries bytes both ways until either side closes.
func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	<-done
	_, _ = a.Close(), b.Close()
	<-done
}

// use counts the ugts connected: the machine is in use while any is.
func (m *machine) use(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conns += n
	m.busy = time.Now()
}

// The machine's own folders: a command's folder may not go over them.
var system = map[string]bool{"bin": true, "data": true, "dev": true, "etc": true, "lib": true, "mnt": true,
	"proc": true, "root": true, "run": true, "sbin": true, "srv": true, "sys": true, "usr": true, "var": true}

// share shares a folder of the user's at its own path in the machine, on a
// device of its own, for as long as any command uses it.
func (m *machine) share(dir string) error {
	first, _, _ := strings.Cut(strings.TrimPrefix(filepath.Clean(dir), "/"), "/")
	if !filepath.IsAbs(dir) || first == "" || system[first] {
		return fmt.Errorf("UGT's machine cannot use %s: run the command from one of your own folders", dir)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.shares[dir]; s != nil {
		s.uses++
		return nil
	}
	device := m.freeDevice()
	if device < 0 {
		return fmt.Errorf("UGT's machine is sharing %d commands' folders already: try again once one has finished", folderDevices)
	}
	shared, err := vz.NewSharedDirectory(dir, false)
	if err != nil {
		return err
	}
	single, err := vz.NewSingleDirectoryShare(shared)
	if err != nil {
		return err
	}
	if err := m.folders[device].SetDirectoryShare(single); err != nil {
		return err
	}
	d := quote(dir)
	if _, err := m.run("mkdir -p " + d + " && mount -t virtiofs " + folderTag(device) + " " + d); err != nil {
		m.empty(device)
		return err
	}
	m.shares[dir] = &share{device: device, uses: 1}
	return nil
}

// freeDevice is a device no command's folder is on, or -1; mu is held.
func (m *machine) freeDevice() int {
	used := map[int]bool{}
	for _, s := range m.shares {
		used[s.device] = true
	}
	for i := range m.folders {
		if !used[i] {
			return i
		}
	}
	return -1
}

// release stops sharing a folder once no command uses it.
func (m *machine) release(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.shares[dir]
	if s == nil {
		return
	}
	if s.uses--; s.uses > 0 {
		return
	}
	if _, err := m.run("umount " + quote(dir)); err != nil {
		fmt.Fprintf(os.Stderr, "ugt: stop sharing %s: %v\n", dir, err)
	}
	delete(m.shares, dir)
	m.empty(s.device)
}

// empty takes a command's folder off its device, once it is unmounted.
func (m *machine) empty(device int) {
	share, err := m.emptyShare()
	if err == nil {
		err = m.folders[device].SetDirectoryShare(share)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ugt: empty the machine's folder device %d: %v\n", device+1, err)
	}
}

// follow forwards each port a container publishes on the machine, from the
// Mac's 127.0.0.1 to the machine's, as containers start and stop, and counts
// the containers running.
func (m *machine) follow() {
	for {
		m.reconcile()
		s, err := m.ssh.NewSession()
		if err != nil {
			return // the machine is stopping
		}
		out, err := s.StdoutPipe()
		if err == nil {
			err = s.Start("docker events --filter type=container --format '{{.Status}}'")
		}
		if err == nil {
			for lines := bufio.NewScanner(out); lines.Scan(); {
				m.reconcile()
			}
		}
		_ = s.Close()
		select {
		case <-m.stopped:
			return
		case <-time.After(time.Second): // the engine restarted: follow it again
		}
	}
}

// published matches a port a container publishes on the machine's loopback
// or every address: either way, the Mac's 127.0.0.1 is where it opens.
var published = regexp.MustCompile(`(?:127\.0\.0\.1|0\.0\.0\.0):(\d+)->\d+/tcp`)

func (m *machine) reconcile() {
	out, err := m.run("docker ps --format 'container {{.Ports}}'")
	if err != nil {
		return
	}
	want := map[int]bool{}
	running := 0
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "container") {
			continue
		}
		running++
		for _, match := range published.FindAllStringSubmatch(line, -1) {
			if port, err := strconv.Atoi(match[1]); err == nil {
				want[port] = true
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return
	}
	m.running = running
	if running > 0 {
		m.busy = time.Now()
	}
	for port, ln := range m.forwards {
		if !want[port] {
			_ = ln.Close()
			delete(m.forwards, port)
		}
	}
	for port := range want {
		if m.forwards[port] != nil {
			continue
		}
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			fmt.Fprintf(os.Stderr, "ugt: open 127.0.0.1:%d on this Mac: %v\n", port, err)
			continue
		}
		m.forwards[port] = ln
		go m.forward(ln, port)
	}
}

// forward carries each connection to the Mac's port to the machine's, over
// SSH.
func (m *machine) forward(ln net.Listener, port int) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			guest, err := m.ssh.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err != nil {
				_ = conn.Close()
				return
			}
			pipe(conn, guest)
		}()
	}
}

// watch runs until the machine stops. Every few seconds it sets the clock
// again if the Mac slept, takes its socket back if something removed it, and
// powers the machine off after five idle minutes.
func (m *machine) watch() error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	last := time.Now()
	for {
		select {
		case <-m.done:
			return nil
		case <-m.stopped:
			fmt.Fprintln(os.Stderr, "ugt: the machine stopped")
			m.shutdown()
			return nil
		case <-signals:
			m.shutdown()
			return nil
		case <-tick.C:
		}
		now := time.Now()
		// the Mac's monotonic clock stands still while it sleeps; its wall clock does not
		if slept := now.Round(0).Sub(last.Round(0)) - now.Sub(last); slept > 2*time.Second || slept < -2*time.Second {
			m.setClock()
		}
		last = now
		if _, err := os.Stat(m.p.socket()); err != nil {
			if err := m.listen(); err == nil {
				go m.accept(m.ln)
			}
		}
		m.mu.Lock()
		quiet := m.conns == 0 && m.running == 0 && time.Since(m.busy) >= idle
		m.mu.Unlock()
		if quiet {
			fmt.Fprintln(os.Stderr, "ugt: idle for five minutes: powering the machine off")
			m.shutdown()
			return nil
		}
	}
}

// shutdown stops taking requests, stops the engine and then the machine
// cleanly, and returns once it has stopped.
func (m *machine) shutdown() {
	m.once.Do(func() {
		m.closeSocket()
		m.mu.Lock()
		for _, ln := range m.forwards {
			_ = ln.Close()
		}
		m.mu.Unlock()
		if m.vm != nil && m.vm.State() == vz.VirtualMachineStateRunning {
			if m.ssh != nil {
				if s, err := m.ssh.NewSession(); err == nil {
					_ = s.Start(quote(m.cfg.Poweroff)) // its session ends with the machine
				}
			}
			select {
			case <-m.stopped:
			case <-time.After(30 * time.Second):
				fmt.Fprintln(os.Stderr, "ugt: the machine did not power off within 30 seconds: stopping it")
				_ = m.vm.Stop()
				<-m.stopped
			}
		}
		close(m.done)
	})
	<-m.done
}
