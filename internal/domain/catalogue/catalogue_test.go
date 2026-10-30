package catalogue

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestFromFolder(t *testing.T) {
	e := FromFolder(os.DirFS("../../../spec/recipe/v0/examples/valid/example-job"), "example/tool/markdown", Published)
	if e.Problem != "" || e.Recipe != "example/tool/markdown" || e.Kind != "job" || len(e.Revision) != 12 || len(e.Inputs) != 1 {
		t.Fatalf("entry %+v", e)
	}
	bad := FromFolder(os.DirFS("../../../spec/recipe/v0/examples/invalid/privileged"), "x/y/z", Local)
	if bad.Problem == "" || bad.Recipe != "x/y/z" {
		t.Fatalf("an invalid folder must carry its problem: %+v", bad)
	}
}

func TestResolve(t *testing.T) {
	ix := Merge(
		[]Entry{{Recipe: "openai/whisper/subtitles", What: "mine", Source: Local}},
		[]Entry{
			{Recipe: "openai/whisper/subtitles", What: "built in", Source: Published},
			{Recipe: "openai/whisper/translate", What: "English subtitles", Source: Published},
			{Recipe: "firecrawl/firecrawl/markdown", What: "Web pages to Markdown", Source: Published},
		},
	)
	if len(ix) != 3 {
		t.Fatalf("merge kept %d entries", len(ix))
	}
	if e, err := ix.Resolve("Firecrawl", "firecrawl", ""); err != nil || e.Recipe != "firecrawl/firecrawl/markdown" {
		t.Errorf("one recipe: %+v, %v", e, err)
	}
	if e, err := ix.Resolve("openai", "whisper", "subtitles"); err != nil || e.What != "mine" {
		t.Errorf("the local recipe wins: %+v, %v", e, err)
	}
	var choose *ChooseTask
	if _, err := ix.Resolve("openai", "whisper", ""); !errors.As(err, &choose) || len(choose.Choices) != 2 {
		t.Errorf("several recipes: %v", err)
	}
	var none *NoRecipe
	if _, err := ix.Resolve("openai", "whisper", "summary"); !errors.As(err, &none) || !strings.Contains(err.Error(), "subtitles, translate") {
		t.Errorf("unknown task: %v", err)
	}
	if _, err := ix.Resolve("someone", "else", ""); !errors.As(err, &none) || none.Project != "someone/else" {
		t.Errorf("no recipe: %v", err)
	}
	if got := ix.Search("markdown web"); len(got) != 1 || got[0].Recipe != "firecrawl/firecrawl/markdown" {
		t.Errorf("search: %+v", got)
	}
}
