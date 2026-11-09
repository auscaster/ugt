package catalogue

import (
	"encoding/json"
	"fmt"

	"github.com/usegeekthing/ugt/spec"
)

// Jobs is spec/catalogue/v0/jobs.json: the jobs the catalogue groups recipes
// by, each in a category, named as people search for them. A recipe names its
// job in x-job; ugt checks the name when a recipe is admitted and never acts
// on it otherwise.
type Jobs struct {
	Format     string     `json:"format"`
	Categories []Category `json:"categories"`
	Jobs       []Job      `json:"jobs"`
}

// Category is a group of jobs, such as audio and music.
type Category struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	About string `json:"about"`
}

// Job is one kind of job people bring, such as removing vocals from a song.
type Job struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	About    string `json:"about"`
}

// JobsFormat names the jobs list's format.
const JobsFormat = "ugt-jobs-v0"

// jobs is the list ugt ships with, read once; its tests prove it reads.
var jobs = mustJobs(spec.JobsV0)

func mustJobs(data []byte) map[string]Job {
	byID, err := readJobs(data)
	if err != nil {
		panic(err)
	}
	return byID
}

// readJobs reads a jobs list: every job and category named once, and every
// job in a category the list has.
func readJobs(data []byte) (map[string]Job, error) {
	var list Jobs
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("jobs.json: %w", err)
	}
	if list.Format != JobsFormat {
		return nil, fmt.Errorf("jobs.json is %q, not %s", list.Format, JobsFormat)
	}
	categories := map[string]bool{}
	for _, c := range list.Categories {
		if categories[c.ID] {
			return nil, fmt.Errorf("jobs.json names category %q twice", c.ID)
		}
		categories[c.ID] = true
	}
	byID := map[string]Job{}
	for _, j := range list.Jobs {
		if _, ok := byID[j.ID]; ok {
			return nil, fmt.Errorf("jobs.json names job %q twice", j.ID)
		}
		if !categories[j.Category] {
			return nil, fmt.Errorf("jobs.json puts job %q in %q, which is not one of its categories", j.ID, j.Category)
		}
		byID[j.ID] = j
	}
	return byID, nil
}

// HasJob reports whether id is a job in the list ugt ships with.
func HasJob(id string) bool {
	_, ok := jobs[id]
	return ok
}
