package vm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/usegeekthing/ugt/internal/app"
)

// Apple caches inode attributes in the guest even when the host unlinks and
// replaces a file at the same name. Let the reader invalidate those names.
// Ordinary copies still travel through the shared directory.
type sharedFiles struct {
	app.Files
	vm *Runtime
}

func workspaceRemoval(root, p string, recursive bool) (app.Command, error) {
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return app.Command{}, fmt.Errorf("path outside UGT workspaces: %s", p)
	}
	// A remove may be called before the workspace exists. Check each existing
	// parent without following links: a recipe cannot redirect root's unlink.
	const script = `set -eu
root=$1; rel=$2; flag=$3; cd -P -- "$root"; here=$(pwd -P)
while [ "${rel#*/}" != "$rel" ]; do
    component=${rel%%/*}; rel=${rel#*/}
    [ ! -L "$component" ] || { echo 'workspace parent is a symbolic link' >&2; exit 1; }
    [ -e "$component" ] || exit 0
    here=$here/$component; cd -P -- "$component"
    [ "$(pwd -P)" = "$here" ] || exit 1
done
rm "$flag" -- "$rel"`
	flag := "-f"
	if recursive {
		flag = "-rf"
	}
	return app.Command{Name: "sh", Args: []string{"-c", script, "ugt", root, filepath.ToSlash(rel), flag}}, nil
}

func (f sharedFiles) remove(p string, recursive bool) error {
	cmd, err := workspaceRemoval(f.vm.cfg.Workspaces, p, recursive)
	if err != nil {
		return err
	}
	var log bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd.Stderr = &log
	code, err := f.vm.Exec(ctx, cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("remove %s in UGT's machine (exit %d): %s", p, code, strings.TrimSpace(log.String()))
	}
	return nil
}

func (f sharedFiles) RemoveAll(p string) error { return f.remove(p, true) }
func (f sharedFiles) WriteFile(p string, data []byte) error {
	if err := f.remove(p, false); err != nil {
		return err
	}
	// A concurrently replaced entry must not turn this write into a truncation.
	root, err := os.OpenRoot(f.vm.cfg.Workspaces)
	if err != nil {
		return err
	}
	defer root.Close()
	rel, err := filepath.Rel(f.vm.cfg.Workspaces, p)
	if err != nil {
		return err
	}
	file, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (r *Runtime) Files() app.Files { return sharedFiles{Files: r.files, vm: r} }
