//go:build !darwin && !linux && !windows

package keychain

import "context"

func (Keychain) Get(context.Context, string, string) (string, bool, error) {
	return "", false, errUnsupported
}
func (Keychain) Set(context.Context, string, string, string) error { return errUnsupported }
func (Keychain) Delete(context.Context, string, string) error      { return errUnsupported }
