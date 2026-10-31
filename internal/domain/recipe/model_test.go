package recipe

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestParseID(t *testing.T) {
	cases := []struct {
		in, want, err string
	}{
		{"openai/whisper/subtitles", "openai/whisper/subtitles", ""},
		{"heygen-com/hyperframes/video", "heygen-com/hyperframes/video", ""},
		{"socketio/socket.io/chat-server", "socketio/socket.io/chat-server", ""},
		{"openai-whisper", "", "not owner/repo/task"},
		{"openai/whisper", "", "not owner/repo/task"},
		{"OpenAI/whisper/subtitles", "", "owner and repo"},
		{"openai/whisper/Subtitles", "", "the task"},
		{"openai/whisper/sub_titles", "", "the task"},
	}
	for _, c := range cases {
		id, err := ParseID(c.in)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%q: got %v, want error containing %q", c.in, err, c.err)
			}
			continue
		}
		if err != nil || id.String() != c.want {
			t.Errorf("%q: got %v, %v", c.in, id, err)
		}
	}
}

func TestParseProject(t *testing.T) {
	for in, want := range map[string]string{
		"openai/whisper":                         "openai/whisper",
		"https://github.com/openai/whisper":      "openai/whisper",
		"github.com/Firecrawl/firecrawl.git":     "Firecrawl/firecrawl",
		"https://github.com/a/b/tree/main/docs/": "a/b",
		"openai/whisper/subtitles":               "",
		"whisper":                                "",
		"https://example.com/a/b":                "",
	} {
		owner, repo, err := ParseProject(in)
		if got := owner + "/" + repo; want == "" && err == nil || want != "" && got != want {
			t.Errorf("ParseProject(%q) = %s, %v; want %q", in, got, err, want)
		}
	}
}

func TestIDNames(t *testing.T) {
	id := ID{Owner: "socketio", Repo: "socket.io", Task: "chat-server"}
	if got := id.Project(); got != "ugt-socketio-socket-io-chat-server" {
		t.Errorf("Project() = %q", got)
	}
	if got := id.Image("0123456789ab"); got != "ugt/socketio/socket.io/chat-server:0123456789ab" {
		t.Errorf("Image() = %q", got)
	}
}

// A valid recipe is admitted to the catalogue when filed under its own ID.
func TestAdmit(t *testing.T) {
	valid := filepath.Join(examples, "valid", "example-job")
	fsys := fstest.MapFS{}
	for _, name := range []string{RecordFile, ComposeFile, "Dockerfile"} {
		data, err := os.ReadFile(filepath.Join(valid, name))
		if err != nil {
			t.Fatal(err)
		}
		fsys["example/job/run/"+name] = &fstest.MapFile{Data: data}
	}
	var record map[string]any
	if err := json.Unmarshal(fsys["example/job/run/"+RecordFile].Data, &record); err != nil {
		t.Fatal(err)
	}
	record["id"] = "example/job/run"
	fsys["example/job/run/"+RecordFile].Data, _ = json.Marshal(record)
	if problems := Admit(must(fs.Sub(fsys, "example/job/run")), "example/job/run"); len(problems) != 0 {
		t.Fatalf("problems = %q", problems)
	}
	if wrong := Admit(must(fs.Sub(fsys, "example/job/run")), "other/job/run"); len(wrong) != 1 || !strings.Contains(wrong[0], "does not match its folder") {
		t.Fatalf("a recipe filed under another ID: %q", wrong)
	}
}

// Access an adopter declared for its author fails until the author says why.
func TestAccessNeedsAWhy(t *testing.T) {
	dir := filepath.Join(examples, "valid", "example-job")
	fsys := fstest.MapFS{}
	for _, name := range []string{RecordFile, ComposeFile, "Dockerfile"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		fsys[name] = &fstest.MapFile{Data: data}
	}
	var record map[string]any
	if err := json.Unmarshal(fsys[RecordFile].Data, &record); err != nil {
		t.Fatal(err)
	}
	record["access"] = []map[string]string{{"kind": "device", "why": ScaffoldMark + " say why the recipe needs this: services.task uses host devices"}}
	fsys[RecordFile].Data, _ = json.Marshal(record)
	if err := Validate(fsys); err == nil || !strings.Contains(err.Error(), "declares device access without saying why") {
		t.Fatalf("got %v", err)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestRevision(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(RecordFile, `{"id": "a/b/c", "task": "t"}`)
	write(ComposeFile, "services: {}\n")
	write("check/x.sh", "true\n")
	fsys := os.DirFS(dir)
	base, err := Revision(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(base) != 12 {
		t.Fatalf("revision %q is not 12 hex characters", base)
	}

	// Every byte of every file counts, recipe.json's included.
	write(RecordFile, `{"task": "t", "id": "a/b/c"}`)
	record, _ := Revision(fsys)
	if record == base {
		t.Error("changing recipe.json did not change the revision")
	}
	write("check/x.sh", "false\n")
	if got, _ := Revision(fsys); got == record {
		t.Error("changing a check script did not change the revision")
	}
}
