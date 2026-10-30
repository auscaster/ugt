// Package fsstate keeps UGT's records in the state directory: the ledger as
// append-only JSON lines, and one JSON file per job, written atomically.
package fsstate

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/job"
	"github.com/auscaster/ugt/internal/domain/ledger"
)

// Ledger appends entries to a JSON-lines file.
type Ledger struct {
	path string
	mu   sync.Mutex
}

// NewLedger returns a ledger kept at path.
func NewLedger(path string) *Ledger { return &Ledger{path: path} }

var _ app.Ledger = (*Ledger)(nil)

// Record appends one entry.
func (l *Ledger) Record(e ledger.Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	line, err := json.Marshal(e)
	if err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Entries reads every entry, oldest first. A missing ledger is empty.
func (l *Ledger) Entries() ([]ledger.Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var entries []ledger.Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for n := 1; scanner.Scan(); n++ {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var e ledger.Entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", l.path, n, err)
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

// Jobs keeps one JSON file per job in a folder.
type Jobs struct {
	dir string
}

// NewJobs returns a job store in dir.
func NewJobs(dir string) *Jobs { return &Jobs{dir: dir} }

var _ app.Jobs = (*Jobs)(nil)

// Save writes the job atomically: a reader sees the old file or the new one.
func (s *Jobs) Save(j job.Job) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, j.ID+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), s.path(j.ID))
}

// Load reads one job.
func (s *Jobs) Load(id string) (job.Job, error) {
	if strings.ContainsAny(id, `/\.`) {
		return job.Job{}, fmt.Errorf("invalid job ID %q", id)
	}
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return job.Job{}, fmt.Errorf("no job %q", id)
	}
	if err != nil {
		return job.Job{}, err
	}
	var j job.Job
	if err := json.Unmarshal(data, &j); err != nil {
		return job.Job{}, fmt.Errorf("job %s: %w", id, err)
	}
	return j, nil
}

// List returns every job, newest first.
func (s *Jobs) List() ([]job.Job, error) {
	names, err := filepath.Glob(filepath.Join(s.dir, "job_*.json"))
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	var jobs []job.Job
	for _, name := range names {
		j, err := s.Load(strings.TrimSuffix(filepath.Base(name), ".json"))
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (s *Jobs) path(id string) string { return filepath.Join(s.dir, id+".json") }
