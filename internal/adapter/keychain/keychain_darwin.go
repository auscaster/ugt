//go:build darwin

package keychain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// security(1) exits 44 when an item does not exist.
const notFound = 44

// security uses the signed-in account's Keychain even when the process has a
// different HOME. Check the directories before constructing any command.
func securityCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	account, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("identify the macOS account: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(home) || !filepath.IsAbs(account.HomeDir) {
		return nil, errors.New("Keychain unavailable: cannot identify the account home directory")
	}
	active, err := os.Stat(home)
	if err != nil {
		return nil, fmt.Errorf("check HOME before using the Keychain: %w", err)
	}
	registered, err := os.Stat(account.HomeDir)
	if err != nil {
		return nil, fmt.Errorf("check the macOS account home before using the Keychain: %w", err)
	}
	if !os.SameFile(active, registered) {
		return nil, errors.New("Keychain unavailable: HOME differs from the macOS account home directory")
	}
	return exec.CommandContext(ctx, "/usr/bin/security", args...), nil
}

func (Keychain) Get(ctx context.Context, recipe, name string) (string, bool, error) {
	var out, errOut bytes.Buffer
	cmd, err := securityCommand(ctx, "find-generic-password", "-s", Service, "-a", account(recipe, name), "-w")
	if err != nil {
		return "", false, err
	}
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == notFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read %s from the Keychain: %v %s", name, err, strings.TrimSpace(errOut.String()))
	}
	return strings.TrimSuffix(out.String(), "\n"), true, nil
}

// Set stores the secret through security's interactive mode, on its standard
// input, so the value never appears in a process list.
func (Keychain) Set(ctx context.Context, recipe, name, value string) error {
	if strings.ContainsAny(value, "\n\r") {
		return errors.New("a secret is one line")
	}
	quote := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
	line := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", quote(Service), quote(account(recipe, name)), quote(value))
	var errOut bytes.Buffer
	cmd, err := securityCommand(ctx, "-i")
	if err != nil {
		return err
	}
	cmd.Stdin, cmd.Stderr = strings.NewReader(line), &errOut
	if err := cmd.Run(); err != nil || errOut.Len() > 0 {
		return fmt.Errorf("store %s in the Keychain: %v %s", name, err, strings.TrimSpace(errOut.String()))
	}
	return nil
}

func (Keychain) Delete(ctx context.Context, recipe, name string) error {
	cmd, err := securityCommand(ctx, "delete-generic-password", "-s", Service, "-a", account(recipe, name))
	if err != nil {
		return err
	}
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == notFound {
		return nil
	}
	return err
}
