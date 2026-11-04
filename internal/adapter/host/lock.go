package host

import (
	"os"
	"path/filepath"
)

// LockFile holds an exclusive lock until release or process exit. If wait is
// false and another process holds the lock, both return values are nil.
// Keep the file on disk: unlinking it would let callers lock different inodes.
func LockFile(path string, wait bool) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	acquired, err := lock(f, wait)
	if !acquired || err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}
