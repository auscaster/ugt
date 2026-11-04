package host

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Independent helpers share the lock, and an abruptly exiting holder cannot
// strand it. This exercises the native file lock on each supported OS.
func TestProcessLock(t *testing.T) {
	const child = "UGT_TEST_LOCK_HOLDER"
	if path := os.Getenv(child); path != "" {
		release, err := LockFile(path, false)
		if err != nil || release == nil {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stdout, "held")
		_, _ = os.Stdin.Read(make([]byte, 1))
		os.Exit(0) // Deliberately do not release.
	}
	path := filepath.Join(t.TempDir(), "states", "recipe.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessLock$")
	cmd.Env = append(os.Environ(), child+"="+path)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	var msg string
	if _, err := fmt.Fscanln(out, &msg); err != nil || msg != "held" {
		t.Fatalf("holder: %s %v", msg, err)
	}
	if unlock, err := LockFile(path, false); err != nil || unlock != nil {
		t.Fatalf("overlap: lock acquired = %t, err = %v", unlock != nil, err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	unlock, err := LockFile(path, false)
	if err != nil || unlock == nil {
		t.Fatalf("after process exit: %v", err)
	}
	unlock()
}
