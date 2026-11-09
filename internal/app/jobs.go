package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/usegeekthing/ugt/internal/domain/job"
)

// Work is a long operation run as a job. It reports progress and returns the
// job's result.
type Work func(ctx context.Context, progress func(step, message string)) (result any, err error)

// jobs tracks the jobs this process is running. A job lives in the process
// that started it (no daemon) and is saved on every event, so its status
// survives the process; one whose process ended unfinished reads as
// interrupted.
type jobs struct {
	mu      sync.Mutex
	running map[string]*runningJob
}

type runningJob struct {
	mu     sync.Mutex
	job    job.Job
	done   chan struct{}
	cancel context.CancelFunc
}

// Start runs work in the background as a job and returns it at once. The job
// runs under ctx, which must outlive the request that started it.
func (a *App) Start(ctx context.Context, kind, recipeID string, work Work) job.Job {
	a.jobsOnce.Do(func() { a.jobs = &jobs{running: map[string]*runningJob{}} })
	now := a.Now()
	ctx, cancel := context.WithCancel(ctx)
	r := &runningJob{
		job:    job.Job{ID: job.NewID(now), Kind: kind, Recipe: recipeID, State: job.Running, Started: now, Events: []job.Event{}},
		done:   make(chan struct{}),
		cancel: cancel,
	}
	a.jobs.mu.Lock()
	a.jobs.running[r.job.ID] = r
	a.jobs.mu.Unlock()
	a.saveJob(r)

	go func() {
		defer close(r.done)
		defer cancel()
		result, err := work(ctx, func(step, message string) {
			r.mu.Lock()
			r.job.Events = append(r.job.Events, job.Event{Time: a.Now(), Step: step, Message: message})
			r.mu.Unlock()
			a.saveJob(r)
		})
		// Work can return a complete report of a failed operation without an
		// execution error. Keep that report, and use its outcome for the job.
		if completed, ok := result.(interface{ OutcomeError() error }); ok && err == nil {
			err = completed.OutcomeError()
		}
		r.mu.Lock()
		r.job.Ended, r.job.Result = a.Now(), result
		switch {
		case ctx.Err() != nil:
			r.job.State, r.job.Error = job.Cancelled, "cancelled"
		case err != nil:
			r.job.State, r.job.Error = job.Failed, err.Error()
		default:
			r.job.State = job.Succeeded
		}
		r.mu.Unlock()
		a.saveJob(r)
	}()
	return r.snapshot()
}

// Wait returns the job once it finishes, or as it stands after wait.
func (a *App) Wait(id string, wait time.Duration) (job.Job, error) {
	if r := a.runningJob(id); r != nil {
		select {
		case <-r.done:
		case <-time.After(wait):
		}
		return r.snapshot(), nil
	}
	return a.Job(id)
}

// Job returns a job's status: live from this process, or as last saved.
func (a *App) Job(id string) (job.Job, error) {
	if r := a.runningJob(id); r != nil {
		return r.snapshot(), nil
	}
	if a.Jobs == nil {
		return job.Job{}, fmt.Errorf("no job %q", id)
	}
	j, err := a.Jobs.Load(id)
	if err != nil {
		return job.Job{}, err
	}
	if !j.State.Done() {
		// saved as running, but no process here runs it: the one that did ended
		j.State, j.Error = job.Failed, "interrupted: the ugt process running it ended; start it again and it resumes from the build cache"
	}
	return j, nil
}

// RecentJobs lists the jobs saved on this machine, newest first, each as
// Job reports it.
func (a *App) RecentJobs(limit int) ([]job.Job, error) {
	if a.Jobs == nil {
		return nil, nil
	}
	all, err := a.Jobs.List()
	if err != nil {
		return nil, err
	}
	if len(all) > limit {
		all = all[:limit]
	}
	out := make([]job.Job, 0, len(all))
	for _, j := range all {
		if live, err := a.Job(j.ID); err == nil {
			j = live
		}
		out = append(out, j)
	}
	return out, nil
}

// Cancel stops a job this process is running.
func (a *App) Cancel(id string) error {
	r := a.runningJob(id)
	if r == nil {
		return fmt.Errorf("job %s is not running in this ugt process", id)
	}
	r.cancel()
	return nil
}

func (a *App) runningJob(id string) *runningJob {
	if a.jobs == nil {
		return nil
	}
	a.jobs.mu.Lock()
	defer a.jobs.mu.Unlock()
	return a.jobs.running[id]
}

func (r *runningJob) snapshot() job.Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	j := r.job
	j.Events = append([]job.Event(nil), r.job.Events...)
	return j
}

func (a *App) saveJob(r *runningJob) {
	if a.Jobs != nil {
		_ = a.Jobs.Save(r.snapshot())
	}
}
