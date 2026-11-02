package releases

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// replace moves the running ugt aside, which Windows allows where replacing it
// is not, and the new one into its place. A ugt still running from the old
// file keeps it; the next update removes it.
func replace(next, self string) error {
	old := fmt.Sprintf("%s.old-%d", self, time.Now().UnixNano())
	if err := os.Rename(self, old); err != nil {
		return fmt.Errorf("%w: %v", errReplace, err)
	}
	if err := os.Rename(next, self); err != nil {
		_ = os.Rename(old, self)
		return err
	}
	return nil
}

// errReplace is Windows refusing to move the running ugt aside.
var errReplace = errors.New("Windows would not move the running ugt aside")

// cleanOld removes the ugts earlier updates moved aside, those no longer
// running.
func cleanOld(self string) {
	old, _ := filepath.Glob(self + ".old-*")
	for _, f := range old {
		_ = os.Remove(f)
	}
}
