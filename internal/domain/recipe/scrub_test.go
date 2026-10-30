package recipe

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestScrub(t *testing.T) {
	recording := []byte("RIFF\x00\x00WAVE the user's own interview")
	sum := sha256.Sum256(recording)
	files := map[string][]byte{
		"compose.yaml":   []byte("services:\n  task:\n    volumes:\n      - /Users/kam/Movies:/movies\n    environment:\n      KEY: sk-abcdefghijklmnopqrstuvwxyz123456\n"),
		"recipe.json":    []byte(`{"task": "subtitles from C:\\Users\\Kam\\Videos"}`),
		"check/run.sh":   []byte("echo hunter2-secret\n"),
		"check/talk.wav": recording,
		"check/key.pem":  []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n"),
		"Dockerfile":     []byte("FROM python:3.12-slim\n"),
	}
	leaks := Scrub(files, Private{
		Paths:   []string{"/Users/kam", `C:\Users\Kam`},
		Secrets: []string{"hunter2-secret"},
		Inputs:  map[string]string{fmt.Sprintf("%x", sum): "talk.wav"},
	})
	got := make([]string, len(leaks))
	for i, l := range leaks {
		got[i] = l.String()
	}
	want := []string{
		"check/key.pem:1: looks like a private key",
		"check/run.sh:1: holds the value of one of your secrets",
		"check/talk.wav: is a copy of talk.wav",
		"compose.yaml:4: names a folder on this machine (/Users/kam)",
		"compose.yaml:6: looks like an API key",
		"recipe.json:1: names a folder on this machine",
	}
	if len(got) != len(want) {
		t.Fatalf("leaks:\n%s", strings.Join(got, "\n"))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("leak %d = %q, want %q", i, got[i], want[i])
		}
	}
	if clean := Scrub(map[string][]byte{"Dockerfile": []byte("FROM python:3.12-slim\n")}, Private{Paths: []string{"/Users/kam"}}); len(clean) != 0 {
		t.Errorf("a clean recipe leaked %v", clean)
	}
}
