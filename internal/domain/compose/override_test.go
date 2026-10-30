package compose

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOverride(t *testing.T) {
	data, err := Override([]Service{{Name: "task", Builds: true}, {Name: "check"}}, "openai/whisper/subtitles", "0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]struct {
			Labels      map[string]string `yaml:"labels"`
			SecurityOpt []string          `yaml:"security_opt"`
			Build       *struct {
				Labels map[string]string `yaml:"labels"`
			} `yaml:"build"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	task, check := doc.Services["task"], doc.Services["check"]
	if task.Build == nil || task.Build.Labels[LabelRevision] != "0123456789ab" {
		t.Errorf("built service lacks image labels: %s", data)
	}
	if check.Build != nil {
		t.Errorf("a service that does not build gained a build section: %s", data)
	}
	for name, s := range doc.Services {
		if s.Labels[LabelRecipe] != "openai/whisper/subtitles" || len(s.SecurityOpt) != 1 {
			t.Errorf("%s lacks labels or no-new-privileges: %s", name, data)
		}
	}
}

// A service that already sets no-new-privileges must not get it twice:
// Compose rejects duplicate security_opt items.
func TestOverrideAddsNothingTwice(t *testing.T) {
	data, err := Override([]Service{{Name: "browser", SecurityOpt: []string{"no-new-privileges:true"}}}, "a/b/c", "0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Services["browser"]["security_opt"]; ok {
		t.Errorf("security_opt added twice: %s", data)
	}
}
