//go:build linux

package keychain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// secretTool runs secret-tool, libsecret's command for the Secret Service.
func secretTool(ctx context.Context, stdin string, args ...string) (string, int, error) {
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return "", 0, fmt.Errorf("%w: install libsecret-tools (Debian, Ubuntu) or libsecret (Fedora, Arch) for secret-tool", errUnsupported)
	}
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, "secret-tool", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(stdin), &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.String(), exit.ExitCode(), nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("secret-tool: %v %s", err, strings.TrimSpace(errOut.String()))
	}
	return out.String(), 0, nil
}

func (Keychain) Get(ctx context.Context, recipe, name string) (string, bool, error) {
	out, exit, err := secretTool(ctx, "", "lookup", "service", Service, "account", account(recipe, name))
	if err != nil || exit != 0 {
		return "", false, err // lookup exits 1 when there is no such secret
	}
	return strings.TrimSuffix(out, "\n"), true, nil
}

// Set stores the secret, which secret-tool reads from its standard input.
func (Keychain) Set(ctx context.Context, recipe, name, value string) error {
	_, exit, err := secretTool(ctx, value, "store", "--label", "UGT "+account(recipe, name), "service", Service, "account", account(recipe, name))
	if err == nil && exit != 0 {
		err = fmt.Errorf("secret-tool store exited %d: is a keyring unlocked?", exit)
	}
	return err
}

func (Keychain) Delete(ctx context.Context, recipe, name string) error {
	_, _, err := secretTool(ctx, "", "clear", "service", Service, "account", account(recipe, name))
	return err
}
