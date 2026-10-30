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

const catalogue = "../../../recipes"

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

func TestIDNames(t *testing.T) {
	id := ID{Owner: "socketio", Repo: "socket.io", Task: "chat-server"}
	if got := id.Project(); got != "ugt-socketio-socket-io-chat-server" {
		t.Errorf("Project() = %q", got)
	}
	if got := id.Image("0123456789ab"); got != "ugt/socketio/socket.io/chat-server:0123456789ab" {
		t.Errorf("Image() = %q", got)
	}
}

// Every catalogue recipe loads and its ID matches its folder.
func TestCatalogue(t *testing.T) {
	problems, err := CheckCatalogue(os.DirFS(catalogue))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
	f, err := Load(os.DirFS(filepath.Join(catalogue, "openai/whisper/subtitles")))
	if err != nil {
		t.Fatal(err)
	}
	if f.ID.Task != "subtitles" || f.Recipe.Kind != KindJob || len(f.Compose) == 0 {
		t.Fatalf("loaded %+v", f.Recipe)
	}
}

// A valid recipe is admitted to the catalogue only with its evidence.
func TestCatalogueNeedsEvidence(t *testing.T) {
	valid := filepath.Join(examples, "valid", "example-job")
	fsys := fstest.MapFS{}
	for _, name := range []string{RecordFile, ComposeFile} {
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
	delete(record, "verified")
	delete(record, "platforms_tested")
	fsys["example/job/run/"+RecordFile].Data, _ = json.Marshal(record)
	if err := Validate(must(fs.Sub(fsys, "example/job/run"))); err != nil {
		t.Fatalf("a draft without evidence should be valid: %v", err)
	}
	problems, err := CheckCatalogue(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "where it was verified") {
		t.Fatalf("problems = %q", problems)
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

	// Recording a verification, or reordering keys, never changes the revision.
	write(RecordFile, `{"task": "t", "id": "a/b/c", "verified": {"date": "2026-10-30"}, "platforms_tested": ["linux/amd64"]}`)
	if got, _ := Revision(fsys); got != base {
		t.Errorf("evidence changed the revision: %s -> %s", base, got)
	}
	// Anything that runs does.
	write("check/x.sh", "false\n")
	if got, _ := Revision(fsys); got == base {
		t.Error("changing a check script did not change the revision")
	}
}
