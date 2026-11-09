package fsstate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/usegeekthing/ugt/internal/domain/job"
	"github.com/usegeekthing/ugt/internal/domain/ledger"
)

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
	s := NewJobs(dir)
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
