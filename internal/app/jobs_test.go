package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/usegeekthing/ugt/internal/domain/job"
	"github.com/usegeekthing/ugt/internal/domain/report"
)

type memJobs struct{ saved map[string]job.Job }

func (m *memJobs) Save(j job.Job) error            { m.saved[j.ID] = j; return nil }
func (m *memJobs) Load(id string) (job.Job, error) { return m.saved[id], nil }
func (m *memJobs) List() ([]job.Job, error)        { return nil, nil }

func TestJobs(t *testing.T) {
	store := &memJobs{saved: map[string]job.Job{}}
	a := &App{Jobs: store, Now: time.Now}
	release := make(chan struct{})
	j := a.Start(context.Background(), "verify", "a/b/c", func(ctx context.Context, progress func(string, string)) (any, error) {
		progress("build", "started")
		<-release
		return "report", nil
	})
	if j.State != job.Running || !strings.HasPrefix(j.ID, "job_") {
		t.Fatalf("started %+v", j)
	}
	// waiting returns the job as it stands when the wait runs out
	if got, _ := a.Wait(j.ID, 20*time.Millisecond); got.State != job.Running {
		t.Fatalf("waited %+v", got)
	}
	close(release)
	got, _ := a.Wait(j.ID, time.Second)
	if got.State != job.Succeeded || got.Result != "report" || len(got.Events) != 1 {
		t.Fatalf("finished %+v", got)
	}
	if store.saved[j.ID].State != job.Succeeded {
		t.Errorf("not saved: %+v", store.saved[j.ID])
	}

	failed := a.Start(context.Background(), "run", "a/b/c", func(context.Context, func(string, string)) (any, error) {
		return nil, errors.New("no engine")
	})
	if got, _ := a.Wait(failed.ID, time.Second); got.State != job.Failed || got.Error != "no engine" {
		t.Errorf("failed job %+v", got)
	}

	// a job saved as running by a process that has ended reads as interrupted
	store.saved["job_OLD"] = job.Job{ID: "job_OLD", State: job.Running}
	if got, _ := a.Job("job_OLD"); got.State != job.Failed || !strings.Contains(got.Error, "interrupted") {
		t.Errorf("orphan %+v", got)
	}
}

func TestJobStateReflectsTheWorkResult(t *testing.T) {
	failed := report.Step{Name: report.StepBuild, Exit: 1, Log: "Dockerfile is missing"}
	for _, tc := range []struct {
		name    string
		result  any
		state   job.State
		message string
	}{
		{"run", RunResult{Steps: []report.Step{failed}}, job.Failed, "build failed (exit 1)"},
		{"verify", report.Report{Steps: []report.Step{failed}}, job.Failed, "build failed (exit 1)"},
		{"command", ExecResult{Service: "task", Exit: 7}, job.Failed, "task exited with status 7"},
		{"completed", RunResult{Passed: true}, job.Succeeded, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{Now: time.Now}
			j := a.Start(context.Background(), tc.name, "a/b/c", func(context.Context, func(string, string)) (any, error) { return tc.result, nil })
			got, err := a.Wait(j.ID, time.Second)
			if err != nil || got.State != tc.state || got.Error != tc.message || !reflect.DeepEqual(got.Result, tc.result) {
				t.Fatalf("job lost the outcome or its report: %+v, %v", got, err)
			}
		})
	}
}
