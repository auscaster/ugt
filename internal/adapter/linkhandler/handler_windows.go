package linkhandler

import (
	"errors"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Handler is the ugt key under the user's own classes, which Windows reads for
// ugt:// links; writing it needs no admin rights.
type Handler struct {
	root registry.Key
	key  string // under root
}

// New is the handler for the signed-in user; home is not needed on Windows.
func New(string) *Handler {
	return &Handler{root: registry.CURRENT_USER, key: `Software\Classes\` + scheme}
}

// Register points ugt:// links at command, unless they go there already.
func (h *Handler) Register(command []string) (bool, error) {
	// no Windows path holds a double quote, so quoting each word is enough
	words := make([]string, 0, len(command)+1)
	for _, w := range command {
		words = append(words, `"`+w+`"`)
	}
	line := strings.Join(append(words, `"%1"`), " ")
	open, _, err := registry.CreateKey(h.root, h.key+`\shell\open\command`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer open.Close()
	if got, _, err := open.GetStringValue(""); err == nil && got == line {
		return false, nil
	}
	if err := open.SetStringValue("", line); err != nil {
		return false, err
	}
	top, _, err := registry.CreateKey(h.root, h.key, registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer top.Close()
	if err := top.SetStringValue("", "URL:UGT link"); err != nil {
		return false, err
	}
	return true, top.SetStringValue("URL Protocol", "")
}

// Unregister removes the ugt key, innermost first, as Windows requires.
func (h *Handler) Unregister() error {
	for _, k := range []string{`\shell\open\command`, `\shell\open`, `\shell`, ``} {
		if err := registry.DeleteKey(h.root, h.key+k); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
	}
	return nil
}
