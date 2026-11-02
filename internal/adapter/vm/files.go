//go:build darwin || linux

package vm

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/crypto/ssh"
)

// The machine's files, as CI publishes them.
const (
	kernelFile    = "kernel"        // the raw kernel Apple's boot loader takes
	initramfsFile = "initramfs"     // mounts the root under a memory overlay
	rootFile      = "root.squashfs" // the layer, read-only
)

var machineFiles = []string{kernelFile, initramfsFile, rootFile}

// diskSize is the data disk's size: a sparse file, so it takes only what the
// engine's images, containers and volumes use.
const diskSize = 64 << 30

// maxSocketPath is macOS's cap on a Unix socket's path, less one.
const maxSocketPath = 103

// paths are where one revision's machine keeps its files under dir.
type paths struct{ dir, revision string }

func (r *Runtime) paths() paths { return paths{r.cfg.Dir, r.cfg.Revision} }

func (p paths) file(name string) string { return filepath.Join(p.dir, p.revision, name) }
func (p paths) disk() string            { return filepath.Join(p.dir, "data.img") }
func (p paths) key() string             { return filepath.Join(p.dir, "key") }          // UGT's, to sign in
func (p paths) hostKey() string         { return filepath.Join(p.dir, "host_key.pub") } // the machine's, to check it
func (p paths) share() string           { return filepath.Join(p.dir, "share") }        // shared read-only with the machine
func (p paths) log() string             { return filepath.Join(p.dir, "machine.log") }
func (p paths) console() string         { return filepath.Join(p.dir, "console.log") }

// socket is where the background process listens: in dir, or, when that path
// is too long for a socket, in a folder of /tmp named for dir.
func (p paths) socket() string {
	if s := filepath.Join(p.dir, "ugt.sock"); len(s) <= maxSocketPath {
		return s
	}
	sum := sha256.Sum256([]byte(p.dir))
	return filepath.Join("/tmp", fmt.Sprintf("ugt-%d-%x", os.Getuid(), sum[:6]), "ugt.sock")
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func (p paths) hasFiles() bool {
	for _, f := range machineFiles {
		if !exists(p.file(f)) {
			return false
		}
	}
	return true
}

func (p paths) hasKeys() bool {
	return exists(p.key()) && exists(p.hostKey()) &&
		exists(filepath.Join(p.share(), "authorized_keys")) && exists(filepath.Join(p.share(), "ssh_host_ed25519_key"))
}

// installed is whether the machine can start: its files, keys and disk.
func (p paths) installed() bool { return p.hasFiles() && p.hasKeys() && exists(p.disk()) }

// prepare makes the machine's keys and data disk when they are missing, and
// reports whether it made any.
func (p paths) prepare() (bool, error) {
	made := false
	if !p.hasKeys() {
		if err := p.makeKeys(); err != nil {
			return false, fmt.Errorf("make the machine's keys: %w", err)
		}
		made = true
	}
	if !exists(p.disk()) {
		f, err := os.OpenFile(p.disk(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return false, fmt.Errorf("make the machine's data disk: %w", err)
		}
		err = f.Truncate(diskSize) // the machine formats it on its first boot
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(p.disk())
			return false, fmt.Errorf("make the machine's data disk: %w", err)
		}
		made = true
	}
	return made, nil
}

// makeKeys makes UGT's key, which the machine authorises for root, and the
// machine's own host key, which UGT pins, so neither side takes another's.
func (p paths) makeKeys() error {
	if err := os.MkdirAll(p.share(), 0o700); err != nil {
		return err
	}
	client, err := newKey()
	if err != nil {
		return err
	}
	host, err := newKey()
	if err != nil {
		return err
	}
	clientPEM, err := ssh.MarshalPrivateKey(client, "UGT")
	if err != nil {
		return err
	}
	hostPEM, err := ssh.MarshalPrivateKey(host, "UGT's machine")
	if err != nil {
		return err
	}
	clientPub, err := ssh.NewPublicKey(client.Public())
	if err != nil {
		return err
	}
	hostPub, err := ssh.NewPublicKey(host.Public())
	if err != nil {
		return err
	}
	for path, data := range map[string][]byte{
		p.key():     pem.EncodeToMemory(clientPEM),
		p.hostKey(): ssh.MarshalAuthorizedKey(hostPub),
		filepath.Join(p.share(), "authorized_keys"):      ssh.MarshalAuthorizedKey(clientPub),
		filepath.Join(p.share(), "ssh_host_ed25519_key"): pem.EncodeToMemory(hostPEM),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func newKey() (ed25519.PrivateKey, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	return key, err
}

// clientConfig signs in to the machine as root with UGT's key, and accepts
// only the machine's own host key.
func (p paths) clientConfig() (*ssh.ClientConfig, error) {
	keyPEM, err := os.ReadFile(p.key())
	if err != nil {
		return nil, fmt.Errorf("UGT's key for its machine: %w: run ugt setup", err)
	}
	signer, err := ssh.ParsePrivateKey(keyPEM)
	if err != nil {
		return nil, err
	}
	hostPub, err := os.ReadFile(p.hostKey())
	if err != nil {
		return nil, fmt.Errorf("the machine's key: %w: run ugt setup", err)
	}
	host, _, _, _, err := ssh.ParseAuthorizedKey(hostPub)
	if err != nil {
		return nil, err
	}
	return &ssh.ClientConfig{User: "root", Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.FixedHostKey(host), Timeout: handshake}, nil
}

// forgetOthers removes the files of every other revision's machine.
func (p paths) forgetOthers() {
	entries, _ := os.ReadDir(p.dir)
	for _, e := range entries {
		if e.IsDir() && e.Name() != p.revision && exists(filepath.Join(p.dir, e.Name(), rootFile)) {
			_ = os.RemoveAll(filepath.Join(p.dir, e.Name()))
		}
	}
}

// lock takes a lock on a file in dir, waiting for it or not, and returns
// what releases it. The system releases it too when the process ends.
func (p paths) lock(name string, wait bool) (func(), error) {
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(p.dir, name), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	how := syscall.LOCK_EX
	if !wait {
		how |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}

// install downloads this revision's machine files and puts them in place
// whole, so a download that fails leaves nothing half there.
func (r *Runtime) install(ctx context.Context, progress func(string)) error {
	p := r.paths()
	download := filepath.Join(p.dir, "download")
	if err := os.MkdirAll(download, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(download)
	tarball, err := r.fetch(ctx, r.cfg.Machine, runtime.GOARCH, download, progress)
	if err != nil {
		return fmt.Errorf("download UGT's machine %s: %w", r.cfg.Machine, err)
	}
	staging := filepath.Join(p.dir, p.revision+".new")
	_ = os.RemoveAll(staging)
	if err := extract(tarball, staging); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("unpack UGT's machine %s: %w", r.cfg.Machine, err)
	}
	_ = os.RemoveAll(filepath.Join(p.dir, p.revision))
	return os.Rename(staging, filepath.Join(p.dir, p.revision))
}

// extract writes the machine's files from a gzipped tar into dir, and fails
// unless every one of them is there.
func extract(tarball, dir string) error {
	f, err := os.Open(tarball)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	found := map[string]bool{}
	t := tar.NewReader(gz)
	for {
		h, err := t.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(h.Name, "./")
		if h.Typeflag != tar.TypeReg || !isMachineFile(name) {
			continue
		}
		out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, t)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		found[name] = true
	}
	for _, name := range machineFiles {
		if !found[name] {
			return fmt.Errorf("it has no %s", name)
		}
	}
	return nil
}

func isMachineFile(name string) bool {
	for _, f := range machineFiles {
		if name == f {
			return true
		}
	}
	return false
}
