package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/domain/recipe"
)

func TestSubmit(t *testing.T) {
	engine := &fakeEngine{config: jobConfig(), services: services(true)}
	a, _ := newApp(t, engine)
	gh := &fakeGitHub{}
	a.GitHub = gh
	ctx := context.Background()

	// a copy of the example, since submit records the evidence in it
	dir := filepath.Join(t.TempDir(), "recipe")
	if err := os.CopyFS(dir, os.DirFS(exampleJob)); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(dir, "check", "notes.txt")
	os.MkdirAll(filepath.Dir(notes), 0o755)
	os.WriteFile(notes, []byte("built in "+filepath.Join(filepath.Dir(a.Layout.Home), "Downloads")+"\n"), 0o644)

	res, err := a.Submit(ctx, dir, SubmitOptions{Agent: "Claude Opus 5.5"})
	if !errors.Is(err, ErrNotShareable) || len(res.Leaks) != 1 || res.Leaks[0].File != "check/notes.txt" || len(gh.proposed) != 0 {
		t.Fatalf("a personal path must stop it: %+v, %v", res.Leaks, err)
	}
	os.Remove(notes)

	// removing the file made a new revision, so the dry run verifies it
	before, _ := os.ReadFile(filepath.Join(dir, recipe.RecordFile))
	dry, err := a.Submit(ctx, dir, SubmitOptions{Agent: "Claude Opus 5.5", DryRun: true})
	if err != nil || dry.Reused || len(gh.proposed) != 0 || !strings.HasPrefix(dry.Title, "Add recipe example/tool/markdown") {
		t.Fatalf("dry run %+v, %v", dry, err)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, recipe.RecordFile)); string(after) != string(before) {
		t.Error("a dry run changed recipe.json")
	}

	// and the submission reuses that verification of the same revision
	res, err = a.Submit(ctx, dir, SubmitOptions{Agent: "Claude Opus 5.5"})
	if err != nil || !res.Reused || res.PullRequest == "" || len(gh.proposed) != 1 {
		t.Fatalf("submit %+v, %v", res, err)
	}
	p := gh.proposed[0]
	if p.Repo != "auscaster/ugt" || p.Dir != "recipes/example/tool/markdown" || p.Branch != "recipe/example-tool-markdown" || p.Files["compose.yaml"] == nil {
		t.Errorf("proposal %+v", p)
	}
	// the folder goes exactly as verified, and nothing is written into it
	if after, _ := os.ReadFile(filepath.Join(dir, recipe.RecordFile)); string(after) != string(before) || string(p.Files[recipe.RecordFile]) != string(before) {
		t.Error("submission changed recipe.json")
	}
	if rev, _ := recipe.Revision(os.DirFS(dir)); rev != res.Revision {
		t.Errorf("the revision changed: %s -> %s", res.Revision, rev)
	}
	if !strings.Contains(p.Body, "| sample-check | 0 |") || !strings.Contains(p.Body, "rejected 6 of 6 broken outputs") || !strings.Contains(p.Body, "by Claude Opus 5.5") {
		t.Errorf("body:\n%s", p.Body)
	}
}
