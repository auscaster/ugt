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
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/usegeekthing/ugt/internal/app"
	"github.com/usegeekthing/ugt/internal/domain/job"
	"github.com/usegeekthing/ugt/internal/domain/ledger"
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
	dir      string
	lockFile func(string, bool) (func(), error)
}

// NewJobs returns a job store in dir.
func NewJobs(dir string, lockFile func(string, bool) (func(), error)) *Jobs {
	return &Jobs{dir: dir, lockFile: lockFile}
}

var _ app.Jobs = (*Jobs)(nil)

// TryLock uses the same process-lifetime lock as settings and workspaces.
// Keep the lock file after releasing it so all observers lock the same inode.
func (s *Jobs) TryLock(id string) (func(), error) {
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return nil, fmt.Errorf("invalid job ID %q", id)
	}
	return s.lockFile(s.path(id)+".lock", false)
}

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

// Values stores value settings in one JSON file: recipe, then name, to value.
type Values struct {
	file     string
	lockFile func(string, bool) (func(), error)
}

// NewValues returns the value store in file.
func NewValues(file string, lockFile func(string, bool) (func(), error)) *Values {
	return &Values{file: file, lockFile: lockFile}
}

var _ app.Values = (*Values)(nil)

func (v *Values) read() (map[string]map[string]string, error) {
	all, err := readJSON[map[string]map[string]string](v.file)
	if all == nil && err == nil {
		all = map[string]map[string]string{}
	}
	return all, err
}

func (v *Values) write(all map[string]map[string]string) error { return writeJSON(v.file, all) }

// readJSON reads a JSON file; a file not there yet reads as the zero value.
func readJSON[T any](file string) (T, error) {
	var v T
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return v, fmt.Errorf("%s: %w", file, err)
	}
	return v, nil
}

// writeJSON replaces a JSON file in one step, readable only by the user.
func writeJSON(file string, v any) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

func (v *Values) Get(recipe string) (map[string]string, error) {
	all, err := v.read()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, val := range all[recipe] {
		out[k] = val
	}
	return out, nil
}

func (v *Values) Set(recipe, name, value string) error {
	unlock, err := v.lockFile(v.file+".lock", true)
	if err != nil {
		return err
	}
	defer unlock()
	all, err := v.read()
	if err != nil {
		return err
	}
	if all[recipe] == nil {
		all[recipe] = map[string]string{}
	}
	all[recipe][name] = value
	return v.write(all)
}

func (v *Values) Delete(recipe, name string) error {
	unlock, err := v.lockFile(v.file+".lock", true)
	if err != nil {
		return err
	}
	defer unlock()
	all, err := v.read()
	if err != nil {
		return err
	}
	delete(all[recipe], name)
	if len(all[recipe]) == 0 {
		delete(all, recipe)
	}
	return v.write(all)
}

// Consents stores the access the user agreed to in one JSON file: recipe, to
// the digests of the grants they agreed to.
type Consents struct {
	file     string
	lockFile func(string, bool) (func(), error)
}

// NewConsents returns the consent store in file.
func NewConsents(file string, lockFile func(string, bool) (func(), error)) *Consents {
	return &Consents{file: file, lockFile: lockFile}
}

var _ app.Consents = (*Consents)(nil)

func (c *Consents) Agreed(recipe, digest string) (bool, error) {
	all, err := readJSON[map[string][]string](c.file)
	if err != nil {
		return false, err
	}
	return slices.Contains(all[recipe], digest), nil
}

func (c *Consents) Agree(recipe, digest string) error {
	unlock, err := c.lockFile(c.file+".lock", true)
	if err != nil {
		return err
	}
	defer unlock()
	all, err := readJSON[map[string][]string](c.file)
	if err != nil {
		return err
	}
	if all == nil {
		all = map[string][]string{}
	}
	if !slices.Contains(all[recipe], digest) {
		all[recipe] = append(all[recipe], digest)
	}
	return writeJSON(c.file, all)
}
