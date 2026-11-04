package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/ledger"
)

func runApp(t *testing.T, engine *fakeEngine) (*App, *memLedger, string) {
	t.Helper()
	a, led := newApp(t, engine)
	a.Catalogues = []Catalogue{&fakeCatalogue{source: catalogue.Published, dirs: map[string]string{"example/tool/markdown": exampleJob}}}
	return a, led, a.Layout.Home
}

func userFile(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("a document"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunDeliversResults(t *testing.T) {
	engine := &fakeEngine{config: jobConfig(), services: services(true)}
	a, led, home := runApp(t, engine)
	// a bare file name is found in the Home folder
	userFile(t, home, "report.docx")
	res, err := a.Run(context.Background(), RunOptions{Project: "example/tool", Inputs: []string{"report.docx"}})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "example", "tool", "markdown")
	if !res.Passed || res.Results != want || strings.Join(res.Files, ",") != "document.md" || res.Source != catalogue.Published {
		t.Fatalf("result %+v", res)
	}
	if data, err := os.ReadFile(filepath.Join(want, "document.md")); err != nil || !strings.HasPrefix(string(data), "# Expected heading") {
		t.Errorf("delivered %q, %v", data, err)
	}
	if got := strings.Join(engine.ran, " "); got != "build task check" {
		t.Errorf("ran %s", got)
	}
	// an image may run as any user, so its services can write both folders
	for _, d := range []string{"inputs", "outputs"} {
		info, err := os.Stat(filepath.Join(a.Layout.Workspaces, "example", "tool", "markdown", d))
		if err != nil || info.Mode().Perm() != 0o777 {
			t.Errorf("%s is not shared with the recipe's services: %v %v", d, info.Mode(), err)
		}
	}
	kinds := map[ledger.Kind]int{}
	for _, e := range led.entries {
		kinds[e.Kind]++
	}
	if kinds[ledger.Workspace] != 1 || kinds[ledger.Project] != 1 || kinds[ledger.Image] != 1 {
		t.Errorf("ledger %+v", led.entries)
	}

	// a second run reuses the image
	engine.ran = nil
	if _, err := a.Run(context.Background(), RunOptions{Project: "https://github.com/example/tool", Inputs: []string{"document.docx=" + filepath.Join(home, "report.docx")}}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(engine.ran, " "); got != "task check" {
		t.Errorf("second run ran %s", got)
	}
	if engine.downs != 3 {
		t.Errorf("the second run must take the first down before replacing its folders: %d downs", engine.downs)
	}
}

// Results are copied out only once nothing of the recipe's runs: a service
// still running could change them mid-copy.
func TestRunDeliversNothingWhileTheStackRuns(t *testing.T) {
	engine := &fakeEngine{config: jobConfig(), services: services(true), downErr: errors.New("the engine is busy")}
	a, _, home := runApp(t, engine)
	userFile(t, home, "report.docx")
	_, err := a.Run(context.Background(), RunOptions{Project: "example/tool", Inputs: []string{"report.docx"}})
	if err == nil || !strings.Contains(err.Error(), "before copying its results out") {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "example", "tool", "markdown", "document.md")); !os.IsNotExist(err) {
		t.Errorf("results delivered: %v", err)
	}
}

func TestRunRefuses(t *testing.T) {
	a, _, home := runApp(t, &fakeEngine{config: jobConfig(), services: services(true)})
	doc := userFile(t, home, "report.docx")
	var none *catalogue.NoRecipe
	if _, err := a.Run(context.Background(), RunOptions{Project: "someone/else"}); !errors.As(err, &none) {
		t.Errorf("no recipe: %v", err)
	}
	for _, c := range []struct {
		opts RunOptions
		want string
	}{
		{RunOptions{Project: "example/tool"}, "needs inputs/document.docx: the document"},
		{RunOptions{Project: "example/tool", Inputs: []string{"missing.docx"}}, "found no file missing.docx"},
		{RunOptions{Project: "example/tool", Inputs: []string{doc, doc}}, "more files than it takes"},
		{RunOptions{Project: "example/tool", Inputs: []string{"other.txt=" + doc}}, "is not an input of"},
		{RunOptions{Project: "example/tool", Inputs: []string{doc}, Values: map[string]string{"NOPE": "1"}}, "has no setting NOPE"},
		// a page showed another revision of it than the catalogue has now
		{RunOptions{Project: "example/tool", Inputs: []string{doc}, Revision: "0" + strings.Repeat("1", 63)}, "changed after you were shown it"},
	} {
		if _, err := a.Run(context.Background(), c.opts); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: got %v, want %q", c.opts, err, c.want)
		}
	}
}

func TestRunRefusesWhatDoesNotFit(t *testing.T) {
	cfg := jobConfig()
	task := cfg.Services["task"]
	task.MemoryBytes = 64 << 30
	cfg.Services["task"] = task
	a, _, home := runApp(t, &fakeEngine{config: cfg, services: services(true)})
	a.Engine.(*fakeEngine).memory = 8 << 30
	var refused *NotPlaced
	_, err := a.Run(context.Background(), RunOptions{Project: "example/tool", Inputs: []string{userFile(t, home, "r.docx")}})
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), "64.0 GB") {
		t.Fatalf("got %v", err)
	}
}

func TestFindListRemove(t *testing.T) {
	engine := &fakeEngine{config: jobConfig(), services: services(true), volumesOf: map[string][]string{"ugt-example-tool-markdown": {"ugt-example-tool-markdown_cache"}}}
	a, _, home := runApp(t, engine)
	ctx := context.Background()

	found, err := a.Find(ctx, "github.com/example/tool")
	if err != nil || len(found.Recipes) != 1 || found.Project != "example/tool" {
		t.Fatalf("find %+v, %v", found, err)
	}
	none, _ := a.Find(ctx, "someone/else")
	if len(none.Recipes) != 0 || !strings.Contains(strings.Join(none.Next, " "), "ugt recipe new someone/else <task>") {
		t.Errorf("find nothing %+v", none)
	}
	if words, _ := a.Find(ctx, "markdown"); len(words.Recipes) != 1 {
		t.Errorf("find by words %+v", words)
	}

	if _, err := a.Run(ctx, RunOptions{Project: "example/tool", Inputs: []string{userFile(t, home, "r.docx")}}); err != nil {
		t.Fatal(err)
	}
	list, err := a.List(ctx)
	if err != nil || len(list) != 1 || len(list[0].Images) != 1 || len(list[0].Volumes) != 1 || list[0].Results == "" {
		t.Fatalf("list %+v, %v", list, err)
	}

	removed, err := a.Remove(ctx, "example/tool")
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Done) != 2 || !strings.Contains(strings.Join(removed.Kept, " "), "ugt delete-data example/tool/markdown") {
		t.Errorf("remove %+v", removed)
	}
	if _, err := os.Stat(filepath.Join(home, "example", "tool", "markdown", "document.md")); err != nil {
		t.Errorf("remove touched the results: %v", err)
	}
	// the data stays findable until it is deleted
	deleted, err := a.DeleteData(ctx, "example/tool/markdown")
	if err != nil || len(deleted.Done) != 1 || !strings.Contains(engine.ran[len(engine.ran)-1], "rmvol ugt-example-tool-markdown_cache") {
		t.Fatalf("delete-data %+v, %v, ran %v", deleted, err, engine.ran)
	}
	if _, err := a.Remove(ctx, "example/tool"); err == nil || !strings.Contains(err.Error(), "holds nothing") {
		t.Errorf("after everything is gone: %v", err)
	}
}

// An agent with no file access writes an input as text.
func TestRunTakesTextInputs(t *testing.T) {
	engine := &fakeEngine{config: jobConfig(), services: services(true)}
	a, _, _ := runApp(t, engine)
	res, err := a.Run(context.Background(), RunOptions{Project: "example/tool", Texts: map[string]string{"document.docx": "# A heading\n"}})
	if err != nil || !res.Passed {
		t.Fatalf("run %+v, %v", res, err)
	}
	got, _ := os.ReadFile(filepath.Join(a.Layout.Workspaces, "example", "tool", "markdown", "inputs", "document.docx"))
	if string(got) != "# A heading\n" {
		t.Errorf("input %q", got)
	}
	if _, err := a.Run(context.Background(), RunOptions{Project: "example/tool", Texts: map[string]string{"other.txt": "x"}}); err == nil || !strings.Contains(err.Error(), "it takes document.docx (the document)") {
		t.Errorf("an unknown input must list the inputs: %v", err)
	}
	// the name without its extension is enough when it is unambiguous
	if res, err := a.Run(context.Background(), RunOptions{Project: "example/tool", Texts: map[string]string{"document": "x"}}); err != nil || !res.Passed {
		t.Errorf("an input named by its stem: %+v, %v", res, err)
	}
}

// A published recipe runs the image admission verified, pulled by digest,
// and builds only when none is published for this machine's architecture.
func TestRunPullsTheVerifiedImage(t *testing.T) {
	digest := "sha256:" + strings.Repeat("c", 64)
	for _, c := range []struct {
		arches []string
		pulled bool
	}{{[]string{"amd64", "arm64"}, true}, {[]string{"amd64"}, false}} {
		engine := &fakeEngine{config: jobConfig(), services: services(true)}
		a, _, home := runApp(t, engine)
		cat := a.Catalogues[0].(*fakeCatalogue)
		cat.images = map[string]catalogue.Image{"": {Ref: "ghcr.io/auscaster/ugt/recipes:0123456789ab", Digest: digest, Arches: c.arches}}
		cat.verified = []catalogue.Verification{{Arch: "amd64", Seconds: 75}, {Arch: "arm64", Seconds: 130}}
		var said []string
		res, err := a.Run(context.Background(), RunOptions{Project: "example/tool", Inputs: []string{userFile(t, home, "r.docx")},
			Progress: func(step, message string) { said = append(said, step+": "+message) }})
		if err != nil || !res.Passed {
			t.Fatalf("run %+v, %v", res, err)
		}
		pulled := slices.Contains(engine.ran, "pull ghcr.io/auscaster/ugt/recipes@"+digest)
		if pulled != c.pulled || slices.Contains(engine.ran, "build") == c.pulled || res.Steps[0].Name != map[bool]string{true: "pull", false: "build"}[c.pulled] {
			t.Errorf("arches %v: ran %v, steps %+v", c.arches, engine.ran, res.Steps)
		}
		// a build says what it costs before it starts, from its verification
		if estimate := "build: building it on this machine: about 3 min on a fast machine, once; later runs reuse it"; slices.Contains(said, estimate) == c.pulled {
			t.Errorf("arches %v: said %q", c.arches, said)
		}
	}
}

// A second helper must never replace the input beneath the first helper, even
// when its job table and engine connection are independent.
func TestRunKeepsAnActiveWorkspace(t *testing.T) {
	one := &fakeEngine{config: jobConfig(), services: services(true)}
	a, _, home := runApp(t, one)
	two := &fakeEngine{config: jobConfig(), services: services(true)}
	b, _, _ := runApp(t, two)
	b.Layout = a.Layout
	userFile(t, home, "first.docx")
	userFile(t, home, "second.docx")
	opt := func(name string) RunOptions {
		return RunOptions{Project: "example/tool", Inputs: []string{"document.docx=" + filepath.Join(home, name)}}
	}
	var secondErr error
	task := one.services["task"]
	one.services["task"] = func(dir string, log io.Writer) int {
		_, secondErr = b.Run(context.Background(), opt("second.docx"))
		input, err := os.ReadFile(filepath.Join(dir, "inputs", "document.docx"))
		original, _ := os.ReadFile(filepath.Join(home, "first.docx"))
		if err != nil || string(input) != string(original) {
			t.Errorf("active input replaced: %s %v", input, err)
		}
		return task(dir, log)
	}
	if err := os.WriteFile(filepath.Join(home, "first.docx"), []byte("first document"), 0600); err != nil {
		t.Fatal(err)
	}
	if first, err := a.Run(context.Background(), opt("first.docx")); err != nil || !first.Passed {
		t.Fatalf("first run: %+v %v", first, err)
	}
	if secondErr == nil || !strings.Contains(secondErr.Error(), "already running a request") || len(two.ran) != 0 || two.downs != 0 {
		t.Fatalf("overlap: %v, steps %v, downs %d", secondErr, two.ran, two.downs)
	}
	if next, err := b.Run(context.Background(), opt("second.docx")); err != nil || !next.Passed {
		t.Fatalf("retry: %+v %v", next, err)
	}
}
