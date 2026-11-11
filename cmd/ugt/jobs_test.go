package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/usegeekthing/ugt/internal/adapter/fsstate"
	"github.com/usegeekthing/ugt/internal/adapter/host"
	"github.com/usegeekthing/ugt/internal/app"
	"github.com/usegeekthing/ugt/internal/domain/job"
)

// This child owns the job until the parent supplies input or kills it. The
// observer is a genuinely separate process using the same persisted job store.
func TestJobOwnerProcess(t *testing.T) {
	if os.Getenv("UGT_JOB_OWNER_FIXTURE") != "1" {
		return
	}
	a := &app.App{Jobs: fsstate.NewJobs(os.Getenv("UGT_JOB_FIXTURE_DIR"), host.LockFile), Now: time.Now}
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
			observer := &app.App{Jobs: fsstate.NewJobs(dir, host.LockFile), Now: time.Now}
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
