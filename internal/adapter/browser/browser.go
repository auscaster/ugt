// Package browser opens what UGT shows the user with the system's own
// opener: a web address in their default browser, a folder in their file
// manager. It implements the Browser port. BROWSER names another program to
// open it with, as on Linux.
package browser

import (
	"os"
	"os/exec"
	"runtime"

	"github.com/auscaster/ugt/internal/app"
)

// Browser is the system's opener.
type Browser struct{}

var _ app.Browser = Browser{}

// Open opens target and returns once the opener has started: it never waits
// for the browser.
func (Browser) Open(target string) error {
	cmd := opener(target)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func opener(target string) *exec.Cmd {
	if b := os.Getenv("BROWSER"); b != "" {
		return exec.Command(b, target)
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target)
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	}
	return exec.Command("xdg-open", target)
}
