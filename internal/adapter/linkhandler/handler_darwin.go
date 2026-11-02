package linkhandler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// lsregister registers an app's links with LaunchServices, which sends them.
const lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

// Handler is UGT.app in the user's Applications folder: an AppleScript applet
// whose "open location" handler passes the link to ugt. macOS sends a link to
// an app only, by an Apple event, never to a program's arguments, and
// osacompile, on every Mac, makes the applet with no compiler.
type Handler struct {
	home       string
	launchServ func(flag, app string) error // registers and unregisters it; tests leave the system alone
}

// New is the handler for the user whose home folder is home.
func New(home string) *Handler {
	return &Handler{home: home, launchServ: func(flag, app string) error {
		if out, err := exec.Command(lsregister, flag, app).CombinedOutput(); err != nil {
			return fmt.Errorf("lsregister %s: %v %s", flag, err, out)
		}
		return nil
	}}
}

func (h *Handler) app() string { return filepath.Join(h.home, "Applications", "UGT.app") }
func (h *Handler) marker() string {
	return filepath.Join(h.app(), "Contents", "Resources", "ugt-command")
}

// Register makes UGT.app, unless it already passes links to command.
func (h *Handler) Register(command []string) (bool, error) {
	line := shellLine(command)
	if got, err := os.ReadFile(h.marker()); err == nil && string(got) == line {
		return false, nil
	}
	apps := filepath.Dir(h.app())
	if err := os.MkdirAll(apps, 0o755); err != nil {
		return false, err
	}
	work, err := os.MkdirTemp(apps, ".ugt-link-*")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(work)
	script := filepath.Join(work, "open.applescript")
	// the link goes to ugt in the background, so the applet answers at once
	source := "on open location theLink\n\tdo shell script " + appleString(line+" ") +
		" & quoted form of theLink & \" > /dev/null 2>&1 &\"\nend open location\n"
	if err := os.WriteFile(script, []byte(source), 0o644); err != nil {
		return false, err
	}
	applet := filepath.Join(work, "UGT.app")
	plist := filepath.Join(applet, "Contents", "Info.plist")
	steps := [][]string{
		{"osacompile", "-o", applet, script},
		{"plutil", "-replace", "CFBundleIdentifier", "-string", "com.usegithubthing.ugt.links", plist},
		{"plutil", "-replace", "CFBundleName", "-string", "UGT", plist},
		{"plutil", "-replace", "LSUIElement", "-bool", "YES", plist},
		{"plutil", "-replace", "CFBundleURLTypes", "-json", `[{"CFBundleURLName":"UGT link","CFBundleURLSchemes":["` + scheme + `"]}]`, plist},
	}
	for _, s := range steps {
		if out, err := exec.Command(s[0], s[1:]...).CombinedOutput(); err != nil {
			return false, fmt.Errorf("%s: %v %s", s[0], err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(applet, "Contents", "Resources", "ugt-command"), []byte(line), 0o644); err != nil {
		return false, err
	}
	// its Info.plist changed after osacompile signed it
	if out, err := exec.Command("codesign", "--force", "--sign", "-", applet).CombinedOutput(); err != nil {
		return false, fmt.Errorf("codesign: %v %s", err, out)
	}
	_ = os.RemoveAll(h.app())
	if err := os.Rename(applet, h.app()); err != nil {
		return false, err
	}
	return true, h.launchServ("-f", h.app())
}

// Unregister removes UGT.app and its links.
func (h *Handler) Unregister() error {
	if _, err := os.Stat(h.app()); err != nil {
		return nil
	}
	err := h.launchServ("-u", h.app())
	if rerr := os.RemoveAll(h.app()); err == nil {
		err = rerr
	}
	return err
}

// shellLine is command as one line for sh, every word quoted.
func shellLine(command []string) string {
	words := make([]string, len(command))
	for i, w := range command {
		words[i] = "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
	}
	return strings.Join(words, " ")
}

// appleString is s as an AppleScript string literal.
func appleString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
