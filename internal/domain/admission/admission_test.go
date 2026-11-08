package admission

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/auscaster/ugt/spec"
)

const exampleJob = "../../../spec/recipe/v0/examples/valid/example-job"

// copyOf copies the example job into a fresh folder, to change it.
func copyOf(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(exampleJob)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadAdmissibleRecipe(t *testing.T) {
	dir := copyOf(t)
	script := filepath.Join(dir, "check", "run.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrue\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec := Read(os.DirFS(dir), "example/tool/markdown")
	if len(rec.Problems) != 0 || rec.Entry == nil || rec.Entry.Kind != "job" || len(rec.Revision) != 12 || len(rec.Gated) != 0 {
		t.Fatalf("record %+v", rec)
	}
	paths := make([]string, len(rec.Files))
	for i, f := range rec.Files {
		paths[i] = f.Path
		if f.SHA != BlobSHA(f.Content) || int(f.Size) != len(f.Content) {
			t.Errorf("%s: sha %s, size %d", f.Path, f.SHA, f.Size)
		}
		// Windows file systems keep no executable bit; admission reads a Linux checkout
		if want := map[bool]string{true: "100755", false: "100644"}[f.Path == "check/run.sh" && runtime.GOOS != "windows"]; f.Mode != want {
			t.Errorf("%s: mode %s", f.Path, f.Mode)
		}
	}
	if !slices.IsSorted(paths) || !slices.Contains(paths, "recipe.json") {
		t.Errorf("files %v", paths)
	}
	conforms(t, rec)
}

func TestReadFindsWhatStopsAdmission(t *testing.T) {
	if rec := Read(os.DirFS(exampleJob), "other/tool/markdown"); len(rec.Problems) != 1 || !strings.Contains(rec.Problems[0], "does not match its folder") {
		t.Errorf("filed elsewhere: %+v", rec.Problems)
	}
	if rec := Read(os.DirFS(t.TempDir()), "a/b/c"); len(rec.Problems) == 0 || rec.Entry != nil {
		t.Errorf("no recipe: %+v", rec)
	}
}

// Access a maintainer reviews is named, so the lane can hold it.
func TestReadNamesGatedAccess(t *testing.T) {
	dir := copyOf(t)
	data, _ := os.ReadFile(filepath.Join(dir, "recipe.json"))
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	record["access"] = []map[string]string{{"kind": "internet", "why": "a"}, {"kind": "device", "why": "it flashes a board"}}
	data, _ = json.Marshal(record)
	if err := os.WriteFile(filepath.Join(dir, "recipe.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := Read(os.DirFS(dir), "example/tool/markdown"); !slices.Equal(rec.Gated, []string{"device"}) {
		t.Errorf("gated %v, problems %v", rec.Gated, rec.Problems)
	}
}

// conforms checks a record against the admission schema the service reads.
func conforms(t *testing.T, rec Record) {
	t.Helper()
	c := jsonschema.NewCompiler()
	for url, doc := range map[string][]byte{spec.RecipeV0URL: spec.RecipeV0, spec.AdmissionV0URL: spec.AdmissionV0} {
		parsed, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddResource(url, parsed); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := c.Compile(spec.AdmissionV0URL + "#/$defs/admitted")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(rec)
	inst, _ := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err := schema.Validate(inst); err != nil {
		t.Errorf("the record is outside spec/admission/v0: %v", err)
	}
}

// A recipe's x-job reaches the catalogue as its entry's job when the jobs
// list has it, and stops admission when it doesn't.
func TestReadTheCataloguesJob(t *testing.T) {
	for job, problem := range map[string]bool{"remove-vocals": false, "make-coffee": true} {
		dir := copyOf(t)
		path := filepath.Join(dir, "recipe.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		doc["x-job"] = job
		data, _ = json.Marshal(doc)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		rec := Read(os.DirFS(dir), "example/tool/markdown")
		named := slices.ContainsFunc(rec.Problems, func(p string) bool { return strings.Contains(p, "x-job") })
		if named != problem || rec.Entry == nil || rec.Entry.Job != job {
			t.Errorf("%s: problems %v, entry %+v", job, rec.Problems, rec.Entry)
		}
		conforms(t, rec)
	}
}
