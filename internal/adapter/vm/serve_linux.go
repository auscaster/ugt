//go:build linux

package vm

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/containers/gvisor-tap-vsock/pkg/types"
	"github.com/containers/gvisor-tap-vsock/pkg/virtualnetwork"
)

// qemuMachine runs the machine under QEMU with KVM, entirely as the user.
// Its network is gvisor-tap-vsock in this process, as on a Mac; its folders
// are served by this process over 9P on that network (folders_linux.go); SSH
// reaches it on that network too.
type qemuMachine struct {
	cfg Config
	p   paths

	vn     *virtualnetwork.VirtualNetwork
	qemu   *exec.Cmd
	served []io.Closer // UGT's own folder and the workspaces, from boot

	mu   sync.Mutex
	port int // the next port a command's folder is served on
}

// The ports this process serves the machine's folders on, at the gateway:
// UGT's own and the workspaces from boot, as the machine's init mounts them,
// and commands' folders from folderPorts on.
const (
	ownPort        = 564
	workspacesPort = 565
	folderPorts    = 566
)

func newHypervisor(cfg Config, p paths) (hypervisor, error) {
	if err := KVM(); err != nil {
		return nil, err
	}
	return &qemuMachine{cfg: cfg, p: p, port: folderPorts}, nil
}

// KVM says why this user cannot run UGT's machine on this computer, if they
// cannot: the machine needs /dev/kvm, which a desktop session has, and a
// server or a session over SSH may not.
func KVM() error {
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	switch {
	case err == nil:
		return f.Close()
	case errors.Is(err, os.ErrNotExist):
		return errors.New("UGT's machine needs KVM, which this computer does not have: turn on virtualization in its firmware settings, or set UGT_RUNTIME=host to use a Docker Engine you run")
	case errors.Is(err, os.ErrPermission):
		return errors.New("UGT's machine needs /dev/kvm, which this user cannot open: run `sudo usermod -aG kvm $USER` once, then sign out and in again")
	default:
		return fmt.Errorf("open /dev/kvm: %w", err)
	}
}

// boot starts QEMU: the layer's root read-only under a memory overlay, the
// data disk, the network and the folders in this process, and a console in
// a file. The secret the folders answer reaches the machine's root through
// QEMU's firmware configuration, which no container can read.
func (h *qemuMachine) boot(stopped func()) error {
	p := h.p
	if err := os.MkdirAll(h.cfg.Workspaces, 0o755); err != nil {
		return err
	}
	vn, err := virtualnetwork.New(&types.Configuration{MTU: 1500, Subnet: subnet, GatewayIP: gateway,
		GatewayMacAddress: gatewayMAC, DHCPStaticLeases: map[string]string{machineIP: machineMAC}})
	if err != nil {
		return err
	}
	h.vn = vn
	link := filepath.Join(filepath.Dir(p.socket()), "network.sock")
	_ = os.Remove(link)
	nic, err := net.Listen("unix", link)
	if err != nil {
		return err
	}
	go func() {
		defer nic.Close()
		conn, err := nic.Accept()
		if err == nil {
			err = vn.AcceptQemu(context.Background(), conn)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "ugt: the machine's network ended: %v\n", err)
		}
	}()
	secret, err := newSecret()
	if err != nil {
		return err
	}
	secretFile := filepath.Join(p.dir, "folders.secret")
	if err := os.WriteFile(secretFile, []byte(secret), 0o600); err != nil {
		return err
	}
	for _, f := range []struct {
		port     int
		dir      string
		readOnly bool
	}{{ownPort, p.share(), true}, {workspacesPort, h.cfg.Workspaces, false}} {
		ln, err := vn.Listen("tcp", net.JoinHostPort(gateway, strconv.Itoa(f.port)))
		if err != nil {
			return err
		}
		served, err := serveFolder(ln, f.dir, secret, f.readOnly)
		if err != nil {
			return err
		}
		h.served = append(h.served, served)
	}
	cpus, memory := resources()
	_ = os.Remove(p.console())
	qemu := filepath.Join(p.dir, p.revision, qemuDir)
	h.qemu = exec.Command(filepath.Join(qemu, qemuProgram),
		"-L", qemu, "-nodefaults", "-no-user-config", "-display", "none", "-no-reboot",
		"-machine", qemuBoard(), "-cpu", "host", "-smp", strconv.Itoa(cpus), "-m", strconv.FormatUint(memory>>20, 10)+"M",
		"-kernel", p.file(kernelFile), "-initrd", p.file(initramfsFile),
		"-append", "console=hvc0 root=/dev/vda rootfstype=squashfs overlaytmpfs modules=squashfs,virtio_blk init="+h.cfg.Init+" ugt.vmm=qemu quiet",
		"-drive", "if=none,id=root,format=raw,readonly=on,file="+option(p.file(rootFile)), "-device", "virtio-blk-pci,drive=root",
		"-drive", "if=none,id=data,format=raw,discard=unmap,file="+option(p.disk()), "-device", "virtio-blk-pci,drive=data",
		"-netdev", "stream,id=net,server=off,addr.type=unix,addr.path="+option(link),
		"-device", "virtio-net-pci,netdev=net,romfile=,mac="+machineMAC,
		"-device", "virtio-rng-pci",
		"-device", "virtio-serial-pci", "-chardev", "file,id=console,path="+option(p.console()), "-device", "virtconsole,chardev=console",
		"-fw_cfg", "name=opt/ugt/secret,file="+option(secretFile))
	h.qemu.Stdout, h.qemu.Stderr = os.Stderr, os.Stderr
	h.qemu.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL} // never outlives this process
	started := make(chan error)
	go func() {
		// the signal follows the thread that started QEMU, so that thread lives as long as QEMU
		runtime.LockOSThread()
		if err := h.qemu.Start(); err != nil {
			started <- fmt.Errorf("start QEMU: %w", err)
			return
		}
		started <- nil
		if err := h.qemu.Wait(); err != nil {
			fmt.Fprintf(os.Stderr, "ugt: QEMU ended: %v\n", err)
		}
		for _, s := range h.served {
			_ = s.Close()
		}
		stopped()
	}()
	return <-started
}

// qemuBoard is the machine QEMU makes: a PC, or on arm64 Arm's virtual board
// with the host's own interrupt controller.
func qemuBoard() string {
	if runtime.GOARCH == "arm64" {
		return "virt,accel=kvm,gic-version=host"
	}
	return "q35,accel=kvm"
}

// option is a value in one of QEMU's comma-separated options, where a comma
// is written twice.
func option(value string) string { return strings.ReplaceAll(value, ",", ",,") }

func newSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// resources gives the machine every CPU and half the computer's memory up to
// 8 GB, as on a Mac. KVM gives the machine memory as it uses it.
func resources() (int, uint64) {
	memory := uint64(4 << 30)
	if f, err := os.Open("/proc/meminfo"); err == nil {
		defer f.Close()
		for s := bufio.NewScanner(f); s.Scan(); {
			if kb, ok := strings.CutPrefix(s.Text(), "MemTotal:"); ok {
				if n, err := strconv.ParseUint(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(kb), "kB")), 10, 64); err == nil {
					memory = max(min(n<<10/2, 8<<30), 2<<30)
				}
			}
		}
	}
	return runtime.NumCPU(), memory
}

func (h *qemuMachine) dial() (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return h.vn.DialContextTCP(ctx, net.JoinHostPort(machineIP, "22"))
}

// attach serves a command's folder on a port of its own, with a secret of
// its own, which the machine sends before it mounts the connection.
func (h *qemuMachine) attach(dir string) (func(string) string, func(), error) {
	h.mu.Lock()
	port := h.port
	h.port++
	h.mu.Unlock()
	secret, err := newSecret()
	if err != nil {
		return nil, nil, err
	}
	ln, err := h.vn.Listen("tcp", net.JoinHostPort(gateway, strconv.Itoa(port)))
	if err != nil {
		return nil, nil, err
	}
	served, err := serveFolder(ln, dir, secret, false)
	if err != nil {
		_ = ln.Close()
		return nil, nil, err
	}
	mount := func(target string) string {
		return fmt.Sprintf("UGT_SECRET=%s UGT_TARGET=%s socat TCP:%s:%d EXEC:%s,nofork", secret, target, gateway, port, h.cfg.Mount9p)
	}
	return mount, func() { _ = served.Close() }, nil
}

func (h *qemuMachine) ready(func(string) (string, error)) {}

func (h *qemuMachine) kill() {
	if h.qemu != nil && h.qemu.Process != nil {
		_ = h.qemu.Process.Kill()
	}
}
