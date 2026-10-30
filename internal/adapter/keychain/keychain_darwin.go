//go:build darwin

package keychain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// security(1) exits 44 when an item does not exist.
const notFound = 44

func (Keychain) Get(ctx context.Context, recipe, name string) (string, bool, error) {
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", Service, "-a", account(recipe, name), "-w")
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
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
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "-i")
	cmd.Stdin, cmd.Stderr = strings.NewReader(line), &errOut
	if err := cmd.Run(); err != nil || errOut.Len() > 0 {
		return fmt.Errorf("store %s in the Keychain: %v %s", name, err, strings.TrimSpace(errOut.String()))
	}
	return nil
}

func (Keychain) Delete(ctx context.Context, recipe, name string) error {
	err := exec.CommandContext(ctx, "/usr/bin/security", "delete-generic-password", "-s", Service, "-a", account(recipe, name)).Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == notFound {
		return nil
	}
	return err
}
