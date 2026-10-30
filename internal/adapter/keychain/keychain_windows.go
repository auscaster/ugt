//go:build windows

package keychain

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

var (
	advapi32   = syscall.NewLazyDLL("advapi32.dll")
	credWrite  = advapi32.NewProc("CredWriteW")
	credRead   = advapi32.NewProc("CredReadW")
	credDelete = advapi32.NewProc("CredDeleteW")
	credFree   = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	errorNotFound           = syscall.Errno(1168)
)

// credential is CREDENTIALW.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// target names one secret in Credential Manager: UGT:owner/repo/task/NAME.
func target(recipe, name string) (*uint16, error) {
	return syscall.UTF16PtrFromString(Service + ":" + account(recipe, name))
}

func (Keychain) Get(_ context.Context, recipe, name string) (string, bool, error) {
	t, err := target(recipe, name)
	if err != nil {
		return "", false, err
	}
	var c *credential
	r, _, callErr := credRead.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&c)))
	if r == 0 {
		if errors.Is(callErr, errorNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read %s from Credential Manager: %w", name, callErr)
	}
	defer credFree.Call(uintptr(unsafe.Pointer(c)))
	return string(unsafe.Slice(c.CredentialBlob, c.CredentialBlobSize)), true, nil
}

func (Keychain) Set(_ context.Context, recipe, name, value string) error {
	t, err := target(recipe, name)
	if err != nil {
		return err
	}
	user, err := syscall.UTF16PtrFromString(Service)
	if err != nil {
		return err
	}
	blob := []byte(value)
	c := credential{Type: credTypeGeneric, TargetName: t, Persist: credPersistLocalMachine, UserName: user, CredentialBlobSize: uint32(len(blob))}
	if len(blob) > 0 {
		c.CredentialBlob = &blob[0]
	}
	if r, _, callErr := credWrite.Call(uintptr(unsafe.Pointer(&c)), 0); r == 0 {
		return fmt.Errorf("store %s in Credential Manager: %w", name, callErr)
	}
	return nil
}

func (Keychain) Delete(_ context.Context, recipe, name string) error {
	t, err := target(recipe, name)
	if err != nil {
		return err
	}
	if r, _, callErr := credDelete.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0); r == 0 && !errors.Is(callErr, errorNotFound) {
		return fmt.Errorf("delete %s from Credential Manager: %w", name, callErr)
	}
	return nil
}
