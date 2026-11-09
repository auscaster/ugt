package catalogue

import (
	"bytes"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/usegeekthing/ugt/spec"
)

// The jobs list ugt ships with is in spec/catalogue/v0's jobs format, and reads.
func TestShippedJobs(t *testing.T) {
	c := jsonschema.NewCompiler()
	for url, doc := range map[string][]byte{spec.RecipeV0URL: spec.RecipeV0, spec.CatalogueV0URL: spec.CatalogueV0} {
		parsed, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddResource(url, parsed); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := c.Compile(spec.CatalogueV0URL + "#/$defs/jobs")
	if err != nil {
		t.Fatal(err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(spec.JobsV0))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(inst); err != nil {
		t.Fatalf("jobs.json is outside spec/catalogue/v0: %v", err)
	}
	if !HasJob("remove-vocals") || HasJob("make-coffee") {
		t.Error("HasJob does not answer from the shipped list")
	}
}

// A list naming a job or category twice, or a job in a category it lacks, is refused.
func TestJobsListMistakes(t *testing.T) {
	for name, list := range map[string]string{
		"format":    `{"format":"ugt-jobs-v9","categories":[],"jobs":[]}`,
		"category":  `{"format":"ugt-jobs-v0","categories":[{"id":"audio"},{"id":"audio"}],"jobs":[]}`,
		"job":       `{"format":"ugt-jobs-v0","categories":[{"id":"audio"}],"jobs":[{"id":"a","category":"audio"},{"id":"a","category":"audio"}]}`,
		"elsewhere": `{"format":"ugt-jobs-v0","categories":[{"id":"audio"}],"jobs":[{"id":"a","category":"video"}]}`,
	} {
		if _, err := readJobs([]byte(list)); err == nil || !strings.Contains(err.Error(), "jobs.json") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
