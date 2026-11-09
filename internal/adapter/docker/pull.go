package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/usegeekthing/ugt/internal/app"
)

// Pull uses Compose's plain progress on a single, pinned image. Unlike
// docker pull, Compose reports bytes even when the runtime has no terminal.
func (e *Engine) Pull(ctx context.Context, ref string, log io.Writer, progress func(string)) error {
	reader, writer := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		readPullProgress(reader, log, progress, time.Now)
	}()
	exit, err := e.runtime.Exec(ctx, app.Command{
		Name: "docker", Args: []string{"compose", "--progress", "plain", "-p", "ugt-pull", "-f", "-", "pull"},
		Stdin:  strings.NewReader(fmt.Sprintf("services:\n  image:\n    image: %q\n", ref)),
		Stdout: writer, Stderr: writer,
	})
	_ = writer.Close()
	<-done
	return check(exit, err)
}

// A slow pull can repeat one byte count many times per second. Retain
// diagnostics and phase changes, and sample only fresh download counts.
var layerProgress = regexp.MustCompile(`^([a-f0-9]{12})\s+(Downloading|Extracting)\s+(?:\[[^]]*\]\s*)?(.+)$`)

func readPullProgress(r io.Reader, log io.Writer, progress func(string), now func() time.Time) {
	lines := bufio.NewReader(r)
	last := make(map[string]string)
	var next time.Time
	for {
		line, err := lines.ReadString('\n')
		fields := layerProgress.FindStringSubmatch(strings.TrimSpace(line))
		if len(fields) == 0 {
			io.WriteString(log, line)
		} else if at := now(); !at.Before(next) {
			message := fmt.Sprintf("%s layer %s: %s", fields[2], fields[1], fields[3])
			if last[fields[1]] != message {
				fmt.Fprintln(log, message)
				progress(message)
				last[fields[1]] = message
				next = at.Add(15 * time.Second)
			}
		}
		if err != nil {
			return
		}
	}
}
