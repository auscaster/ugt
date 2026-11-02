// Package layer is UGT's Linux layer: Alpine Linux with Docker Engine, Compose
// and Buildx. CI builds it once per revision of the files it is built from,
// Dockerfile, provision.sh and machine/, and publishes it as one image with
// one file system layer. Windows imports it as UGT's WSL distro and updates it
// in place with update.sh; UGT's Mac machine boots it, from the files CI
// publishes beside it; verification runs recipes inside it. So a recipe runs
// on exactly the engine it was verified on.
package layer

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"strings"
	"sync"
)

const (
	// Distro is the WSL distro UGT runs its engine in.
	Distro = "UGT"
	// EngineCommand starts the engine if it is not running, and waits for it.
	EngineCommand = "/usr/local/bin/ugt-engine"
	// Workspaces is where recipe workspaces live in the layer.
	Workspaces = "/var/lib/ugt/workspaces"
	// RevisionFile records, in the layer, the revision it was built from.
	RevisionFile = "/usr/share/ugt/revision"
	// Repository is where CI publishes the layer, tagged by revision.
	Repository = "ghcr.io/auscaster/ugt/layer"
)

// built holds the files the layer is built from: its revision is theirs.
//
//go:embed Dockerfile provision.sh machine
var built embed.FS

//go:embed update.sh
var update string

// Revision names the layer: a digest of the files it is built from, so each
// layer is built and published once, and any change to them is a new layer.
var Revision = sync.OnceValue(func() string {
	h := sha256.New()
	_ = fs.WalkDir(built, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := built.ReadFile(p)
		if err != nil {
			return err
		}
		h.Write([]byte(p + "\x00"))
		h.Write(data)
		h.Write([]byte{0})
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:12]
})

// Image is the published layer this ugt runs on.
func Image() string { return Repository + ":" + Revision() }

// Machine is the published files UGT's machine boots, built from the same
// revision as the layer: its kernel, initramfs and root.
func Machine() string { return Image() + "-machine" }

// QEMU is the published QEMU that runs the machine on Linux, built from the
// same revision.
func QEMU() string { return Image() + "-qemu" }

// The machine's own commands, in its root.
const (
	// MachineInit is the machine's PID 1.
	MachineInit = "/usr/local/bin/ugt-init"
	// MachinePoweroff stops the engine, then the machine, cleanly.
	MachinePoweroff = "/usr/local/bin/ugt-poweroff"
)

// Update is the script that updates the layer in place, run as root with sh
// and the new layer's file system, a gzipped tar, on stdin. Line endings are
// LF whatever the checkout did to them.
func Update() string { return strings.ReplaceAll(update, "\r\n", "\n") }
