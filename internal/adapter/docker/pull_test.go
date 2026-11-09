package docker

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/usegeekthing/ugt/internal/app"
)

func TestPullProgressReachesTheCallerBeforeTheCommandEnds(t *testing.T) {
	progress := make(chan string, 1)
	engine := New(pullCommand(func(c app.Command) (int, error) {
		s, _ := io.ReadAll(c.Stdin)
		if !strings.Contains(string(s), "image: \"registry.test/app@sha256:abc\"") {
			t.Errorf("pull input: %s", s)
		}
		io.WriteString(c.Stderr, " fd1234567890 Down")
		io.WriteString(c.Stderr, "loading 1.049MB\n")
		select {
		case got := <-progress:
			if got != "Downloading layer fd1234567890: 1.049MB" {
				t.Errorf("progress: %s", got)
			}
		case <-time.After(3 * time.Second):
			t.Error("no live progress")
		}
		io.WriteString(c.Stderr, "registry denied the request")
		return 1, nil
	}))
	var log bytes.Buffer
	if err := engine.Pull(context.Background(), "registry.test/app@sha256:abc", &log, func(s string) { progress <- s }); err == nil || !strings.Contains(log.String(), "registry denied") {
		t.Fatalf("failed pull lost diagnostic: %v %s", err, log.String())
	}
}

type pullCommand func(app.Command) (int, error)

func (f pullCommand) Exec(_ context.Context, c app.Command) (int, error) { return f(c) }

func TestRepeatedByteCountsAreNotProgress(t *testing.T) {
	lines := " 000000000001 Downloading 1MB\n 000000000001 Downloading 2MB\n 000000000001 Downloading 1MB\n 000000000002 Downloading 3MB\n"
	at := time.Unix(0, 0)
	var updates []string
	readPullProgress(strings.NewReader(lines), io.Discard, func(s string) { updates = append(updates, s) }, func() time.Time {
		at = at.Add(8 * time.Second)
		return at
	})
	if len(updates) != 2 || !strings.HasSuffix(updates[0], "1MB") || !strings.HasSuffix(updates[1], "3MB") {
		t.Errorf("updates: %v", updates)
	}
}
