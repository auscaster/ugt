//go:build (darwin && cgo) || linux

package vm

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

// idle is how long the machine runs with no one using it, and no container
// running, before it powers off, as Docker Desktop's Resource Saver does.
const idle = 5 * time.Minute

// The network UGT runs for the machine, in this process: the machine is .2 and
// the computer .1, as the machine's init sets it.
const (
	subnet     = "192.168.127.0/24"
	gateway    = "192.168.127.1"
	gatewayMAC = "5a:94:ef:e4:0c:dd"
	machineIP  = "192.168.127.2"
	machineMAC = "5a:94:ef:e4:0c:ee"
)

// hypervisor is what runs the machine: Apple's Virtualization framework on a
// Mac, QEMU with KVM on Linux. Everything else about the machine is the same.
type hypervisor interface {
	// boot starts the machine, and calls stopped once it stops, whoever
	// stopped it.
	boot(stopped func()) error
	// dial reaches the machine's sshd.
	dial() (net.Conn, error)
	// attach shares a folder of the user's with the machine. It returns what
	// makes the command that mounts it at a path in the machine, and what
	// takes it back once it is unmounted.
	attach(dir string) (mount func(target string) string, detach func(), err error)
	// ready does what this hypervisor's machine needs once it answers.
	ready(run func(line string) (string, error))
	// kill stops the machine at once.
	kill()
}

// Serve runs UGT's machine until it stops: asked to, idle, or powered off from
// inside. It is the background process every ugt reaches the machine through,
// on its socket. ready is told readyLine once the engine answers, or why the
// machine could not start.
func Serve(cfg Config, ready io.WriteCloser) error {
	// a program the machine runs in, QEMU on Linux, must not hold the ugt
	// waiting for this line open after this process has said it
	if f, ok := ready.(*os.File); ok {
		syscall.CloseOnExec(int(f.Fd()))
	}
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
	if m.hv, err = newHypervisor(cfg, m.p); err != nil {
		return tell(ready, err)
	}
	if err := m.listen(); err != nil {
		return tell(ready, err)
	}
	started := time.Now()
	if err := m.hv.boot(func() { close(m.stopped) }); err != nil {
		m.closeSocket()
		return tell(ready, err)
	}
	m.booted = true
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

	hv     hypervisor
	booted bool
	ssh    *ssh.Client // this process's own connection, for its own commands
	ln     net.Listener

	mu       sync.Mutex
	closing  bool              // shutting down: no new socket or forward
	shares   map[string]*share // by folder on the computer
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
	uses   int
	detach func()
}

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
		if conn, err := m.hv.dial(); err == nil {
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
	m.hv.ready(m.run)
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

// setClock sets the machine's clock to the computer's: at boot, and after the
// computer sleeps, when the machine's clock stood still.
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
		guest, err := m.hv.dial()
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

// share shares a folder of the user's at its own path in the machine, for as
// long as any command uses it.
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
	mount, detach, err := m.hv.attach(dir)
	if err != nil {
		return err
	}
	d := quote(dir)
	if _, err := m.run("mkdir -p " + d + " && " + mount(d)); err != nil {
		detach()
		return err
	}
	m.shares[dir] = &share{uses: 1, detach: detach}
	return nil
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
	s.detach()
}

// follow forwards each port a container publishes on the machine, from the
// computer's 127.0.0.1 to the machine's, as containers start and stop, and
// counts the containers running.
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
// or every address: either way, the computer's 127.0.0.1 is where it opens.
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
			fmt.Fprintf(os.Stderr, "ugt: open 127.0.0.1:%d on this computer: %v\n", port, err)
			continue
		}
		m.forwards[port] = ln
		go m.forward(ln, port)
	}
}

// forward carries each connection to the computer's port to the machine's,
// over SSH.
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
// again if the computer slept, takes its socket back if something removed
// it, and powers the machine off after five idle minutes.
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
		// the monotonic clock stands still while the computer sleeps; its wall clock does not
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
		running := m.booted
		select {
		case <-m.stopped:
			running = false
		default:
		}
		if running {
			if m.ssh != nil {
				if s, err := m.ssh.NewSession(); err == nil {
					_ = s.Start(quote(m.cfg.Poweroff)) // its session ends with the machine
				}
			}
			select {
			case <-m.stopped:
			case <-time.After(30 * time.Second):
				fmt.Fprintln(os.Stderr, "ugt: the machine did not power off within 30 seconds: stopping it")
				m.hv.kill()
				<-m.stopped
			}
		}
		close(m.done)
	})
	<-m.done
}
