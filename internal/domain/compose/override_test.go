package compose

import (
	"fmt"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOverride(t *testing.T) {
	m := Model{Services: map[string]Service{"task": {Build: &Build{}}, "check": {}}}
	data, err := Override(m, "openai/whisper/subtitles", "0123456789ab", 0)
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

// A service that already sets no-new-privileges must not get it twice, since
// Compose rejects duplicate security_opt items; and a privileged service,
// which its recipe declares, runs as declared.
func TestOverrideAddsNoNewPrivilegesOnlyWhereItBelongs(t *testing.T) {
	m := Model{Services: map[string]Service{
		"browser": {SecurityOpt: []string{"no-new-privileges:true"}},
		"flasher": {Privileged: true},
	}}
	data, err := Override(m, "a/b/c", "0123456789ab", 0)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"browser", "flasher"} {
		if _, ok := doc.Services[name]["security_opt"]; ok {
			t.Errorf("%s gained security_opt: %s", name, data)
		}
	}
}

// A recipe may allow more CPUs than the machine has; Docker refuses that, so
// the override lowers the limit to what there is.
func TestOverrideCapsCPUs(t *testing.T) {
	m := Model{Services: map[string]Service{"api": {CPUs: 4}, "worker": {CPUs: 1}, "redis": {}}}
	data, err := Override(m, "a/b/c", "0123456789ab", 2)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(doc.Services["api"]["cpus"]) != "2" || doc.Services["worker"]["cpus"] != nil || doc.Services["redis"]["cpus"] != nil {
		t.Errorf("override %s", data)
	}
}
