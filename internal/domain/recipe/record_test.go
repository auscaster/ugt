package recipe

import (
	"os"
	"strings"
	"testing"
)

func TestSetFieldsKeepsEverythingElse(t *testing.T) {
	original, err := os.ReadFile(examples + "/valid/example-job/recipe.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := SetFields(original, map[string]any{
		"verified":         Verified{Date: "2026-10-30", Agent: "a", Evidence: "e"},
		"platforms_tested": []string{"linux/arm64"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, `"x-note": "Fictional project`) || strings.Index(text, `"id"`) > strings.Index(text, `"repo"`) {
		t.Errorf("lost a field or the order:\n%s", text)
	}
	if !strings.Contains(text, "\"platforms_tested\": [\n    \"linux/arm64\"\n  ]") || !strings.HasSuffix(strings.TrimSpace(text), "}\n}") {
		t.Errorf("fields not set where expected:\n%s", text)
	}
	if err := ValidateRecord(got); err != nil {
		t.Errorf("result is invalid: %v", err)
	}
	// unchanged evidence leaves the file byte for byte as it was
	same, _ := SetFields(original, map[string]any{"platforms_tested": []string{"linux/amd64 on Linux", "linux/arm64 in Docker Desktop on macOS"}})
	if string(same) != string(original) {
		t.Errorf("rewrote an unchanged file:\n%s", same)
	}
}
