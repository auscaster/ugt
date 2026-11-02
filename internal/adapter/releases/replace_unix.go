//go:build !windows

package releases

import "os"

// replace puts the new ugt at self in one rename: a running ugt keeps the
// file it started from, and macOS checks the new one's signature afresh.
func replace(next, self string) error { return os.Rename(next, self) }

// cleanOld has nothing to remove: renaming over a running program is allowed.
func cleanOld(string) {}
