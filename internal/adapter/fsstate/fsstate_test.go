package fsstate

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/usegeekthing/ugt/internal/adapter/host"
	"github.com/usegeekthing/ugt/internal/app"
	"github.com/usegeekthing/ugt/internal/domain/job"
	"github.com/usegeekthing/ugt/internal/domain/ledger"
)

// This child owns the job until the parent supplies input or kills it. The
// observer is a genuinely separate process using the same persisted job store.
func TestJobOwnerProcess(t *testing.T) {
	if os.Getenv("UGT_JOB_OWNER_FIXTURE") != "1" {
		return
	}
	a := &app.App{Jobs: NewJobs(os.Getenv("UGT_JOB_FIXTURE_DIR"), host.LockFile), Now: time.Now}
	ready := make(chan struct{})
	j := a.Start(context.Background(), "run", "example/tool/markdown", func(_ context.Context, progress func(string, string)) (any, error) {
		progress("prepare", "Keep the original request")
		close(ready)
		_, err := bufio.NewReader(os.Stdin).ReadString('\n')
		return map[string]string{"output": "47 blue chairs"}, err
	})
	<-ready
	fmt.Println(j.ID)
	_, _ = a.Wait(j.ID, 10*time.Second)
}

func TestJobStatusAcrossProcesses(t *testing.T) {
	for _, killed := range []bool{false, true} {
		t.Run(fmt.Sprint("killed=", killed), func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestJobOwnerProcess$")
			cmd.Env = append(os.Environ(), "UGT_JOB_OWNER_FIXTURE=1", "UGT_JOB_FIXTURE_DIR="+dir)
			stdin, _ := cmd.StdinPipe()
			stdout, _ := cmd.StdoutPipe()
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			id, err := bufio.NewReader(stdout).ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			id = strings.TrimSpace(id)
			observer := &app.App{Jobs: NewJobs(dir, host.LockFile), Now: time.Now}
			live, err := observer.Job(id)
			if err != nil || live.State != job.Running || len(live.Events) != 1 {
				t.Fatalf("live owner reported as interrupted: %+v, %v", live, err)
			}
			if live, err = observer.Wait(id, 10*time.Millisecond); err != nil || live.State != job.Running {
				t.Fatalf("lost live owner while waiting: %+v %v", live, err)
			}
			if killed {
				if err := cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				_ = cmd.Wait()
			} else {
				sent := make(chan error, 1)
				go func() { time.Sleep(30 * time.Millisecond); _, err := fmt.Fprintln(stdin); sent <- err }()
				if completed, err := observer.Wait(id, 3*time.Second); err != nil || completed.State != job.Succeeded {
					t.Fatalf("did not observe completion: %+v %v", completed, err)
				}
				if err := <-sent; err != nil {
					t.Fatal(err)
				}
				if err := cmd.Wait(); err != nil {
					t.Fatal(err)
				}
			}
			got, err := observer.Job(id)
			if err != nil {
				t.Fatal(err)
			}
			if killed {
				if got.State != job.Failed || !strings.Contains(got.Error, "interrupted") {
					t.Fatal(got)
				}
			} else if got.State != job.Succeeded || got.Result.(map[string]any)["output"] != "47 blue chairs" {
				t.Fatal(got)
			}
		})
	}
}

func TestLedger(t *testing.T) {
	l := NewLedger(filepath.Join(t.TempDir(), "state", "ledger.jsonl"))
	if entries, err := l.Entries(); err != nil || len(entries) != 0 {
		t.Fatalf("a new ledger should be empty: %v %v", entries, err)
	}
	t0 := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	for _, e := range []ledger.Entry{
		{Kind: ledger.Image, Name: "ugt/a/b/c:1", Recipe: "a/b/c", Created: t0},
		{Kind: ledger.Image, Name: "ugt/a/b/c:1", Recipe: "a/b/c", Removed: t0.Add(time.Hour)},
	} {
		if err := l.Record(e); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := l.Entries()
	if err != nil || len(entries) != 2 || !entries[1].Removed.Equal(t0.Add(time.Hour)) {
		t.Fatalf("entries %+v %v", entries, err)
	}
	if live := ledger.Live(entries); len(live) != 0 {
		t.Errorf("removed image still live: %+v", live)
	}
}

func TestJobs(t *testing.T) {
	dir := t.TempDir()
	s := NewJobs(dir, host.LockFile)
	first := job.Job{ID: job.NewID(time.UnixMilli(1)), Kind: "verify", State: job.Running}
	second := job.Job{ID: job.NewID(time.UnixMilli(2)), Kind: "verify", State: job.Succeeded, Result: map[string]any{"passed": true}}
	for _, j := range []job.Job{first, second} {
		if err := s.Save(j); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Load(first.ID)
	if err != nil || got.State != job.Running {
		t.Fatalf("load %+v %v", got, err)
	}
	list, err := s.List()
	if err != nil || len(list) != 2 || list[0].ID != second.ID {
		t.Fatalf("list should be newest first: %+v %v", list, err)
	}
	if _, err := s.Load("../etc/passwd"); err == nil {
		t.Error("a path-like job ID was accepted")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
	if _, err := os.Stat(filepath.Join(dir, first.ID+".json")); err != nil {
		t.Error(err)
	}
}
