package keychain

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func TestSecurityCommandChecksAccountHome(t *testing.T) {
	// Inspect the command, never execute it.
	ctx := context.Background()
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if cmd, err := securityCommand(ctx, "-i"); cmd != nil || err == nil {
		t.Fatal("constructed a Keychain command with a different HOME")
	}
	alias := filepath.Join(tmp, "account-home")
	if err := os.Symlink(account.HomeDir, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", alias)
	if cmd, err := securityCommand(ctx, "-i"); err != nil || cmd == nil {
		t.Fatalf("rejected the same directory through a symlink: %v", err)
	}
}
