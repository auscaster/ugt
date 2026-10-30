// Package keychain implements the Secrets port with the system's own store:
// the macOS Keychain, Windows Credential Manager, and the Secret Service on
// Linux through secret-tool. Secrets are stored under the service "UGT", one
// per recipe and setting, and never written anywhere else.
package keychain

import (
	"errors"

	"github.com/auscaster/ugt/internal/app"
)

// Service is the name UGT's secrets are filed under.
const Service = "UGT"

// Keychain is the system's secret store.
type Keychain struct{}

var _ app.Secrets = Keychain{}

// account names one secret: owner/repo/task/NAME.
func account(recipe, name string) string { return recipe + "/" + name }

var errUnsupported = errors.New("this system has no secret store UGT can use")
