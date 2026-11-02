//go:build !darwin && !windows

package linkhandler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Handler is a desktop entry in the user's applications folder that takes
// ugt:// links, made their default with xdg-mime where the desktop has it.
type Handler struct{ home string }

// New is the handler for the user whose home folder is home.
func New(home string) *Handler { return &Handler{home: home} }

const entry = "ugt-link.desktop"

func (h *Handler) path() string {
	return filepath.Join(h.home, ".local", "share", "applications", entry)
}

// Register writes the entry, unless it already sends links to command.
func (h *Handler) Register(command []string) (bool, error) {
	words := make([]string, len(command))
	for i, w := range command {
		// the entry's values are unescaped once, then Exec's quoting once more
		words[i] = `"` + strings.NewReplacer(`\`, `\\\\`, `"`, `\\"`, "`", "\\\\`", `$`, `\\$`).Replace(w) + `"`
	}
	content := "[Desktop Entry]\nType=Application\nName=UGT\nComment=Opens Open in UGT links\nNoDisplay=true\n" +
		"Exec=" + strings.Join(words, " ") + " %u\nMimeType=x-scheme-handler/" + scheme + ";\n"
	if got, err := os.ReadFile(h.path()); err == nil && string(got) == content {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(h.path()), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(h.path(), []byte(content), 0o644); err != nil {
		return false, err
	}
	if xdg, err := exec.LookPath("xdg-mime"); err == nil {
		_ = exec.Command(xdg, "default", entry, "x-scheme-handler/"+scheme).Run()
	}
	return true, nil
}

// Unregister removes the entry.
func (h *Handler) Unregister() error {
	if err := os.Remove(h.path()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
