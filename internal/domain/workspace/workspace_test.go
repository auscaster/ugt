package workspace

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/auscaster/ugt/internal/domain/recipe"
)

func TestDefaultLayout(t *testing.T) {
	env := func(k string) string {
		if k == "LOCALAPPDATA" {
			return `C:\Users\k\AppData\Local`
		}
		return ""
	}
	cases := map[string]string{
		"darwin":  filepath.Join("/Users/k", "Library", "Application Support", "UGT"),
		"linux":   filepath.Join("/Users/k", ".local", "share", "ugt"),
		"windows": filepath.Join(`C:\Users\k\AppData\Local`, "UGT"),
	}
	for goos, state := range cases {
		l := DefaultLayout(goos, "/Users/k", env)
		if l.State != state || l.Home != filepath.Join("/Users/k", "UGT") {
			t.Errorf("%s: %+v", goos, l)
		}
	}
}

func TestPaths(t *testing.T) {
	l := Layout{Home: "/h/UGT", State: "/s", Workspaces: "/w"}
	id := recipe.ID{Owner: "openai", Repo: "whisper", Task: "subtitles"}
	if got := l.Workspace(id); got != "/w/openai/whisper/subtitles" {
		t.Errorf("Workspace = %q", got)
	}
	if got := l.Results(id); got != filepath.Join("/h/UGT", "openai", "whisper", "subtitles") {
		t.Errorf("Results = %q", got)
	}
	scratch := l.Scratch(id, time.Date(2026, 10, 30, 9, 1, 2, 0, time.UTC))
	if !strings.HasPrefix(scratch, "/w/.scratch/openai.whisper.subtitles-20261030T090102") {
		t.Errorf("Scratch = %q", scratch)
	}
}
