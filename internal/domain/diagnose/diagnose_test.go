package diagnose

import (
	"strings"
	"testing"
)

func TestDiagnose(t *testing.T) {
	cases := []struct {
		name string
		exit int
		log  string
		want string // first rule ID, or "" for none
		line string
	}{
		{"placeholder", 1, "ugt scaffold: write the check service: fail unless outputs/ holds what the task made, for any input\n", "scaffold", "ugt scaffold: write the check service: fail unless outputs/ holds what the task made, for any input"},
		{"oom by exit code", 137, "starting\n", "memory", ""},
		{"oom by log", 1, "worker 3\nOOMKilled: true\n", "memory", "OOMKilled: true"},
		{"oom in a stack's logs", 1, "container ugt-x-api-1 exited (137)\napi  | Killed\n", "memory", "api  | Killed"},
		{"arm64 ffmpeg", 1, "#7 0.4 rosetta error: failed to open elf at /lib64/ld-linux-x86-64.so.2\n", "architecture", "#7 0.4 rosetta error: failed to open elf at /lib64/ld-linux-x86-64.so.2"},
		{"opengl", 1, "ImportError: libGL.so.1: cannot open shared object file\n", "opengl", "ImportError: libGL.so.1: cannot open shared object file"},
		{"dns", 1, "fetch failed: getaddrinfo ENOTFOUND example.com\n", "dns", ""},
		{"healthcheck", 1, "dependency failed to start: container x has no healthcheck configured\n", "healthcheck", ""},
		{"python module", 1, "ModuleNotFoundError: No module named 'trimesh'\n", "python-module", ""},
		{"too many cpus", 1, "Error response from daemon: range of CPUs is from 0.01 to 2.00, as there are only 2 CPUs available\n", "cpus", ""},
		{"nothing known", 2, "syntax error: unexpected end of file\n", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Diagnose(c.exit, c.log)
			if c.want == "" {
				if len(got) != 0 {
					t.Fatalf("expected no findings, got %+v", got)
				}
				return
			}
			if len(got) == 0 || got[0].Rule != c.want {
				t.Fatalf("got %+v, want first rule %q", got, c.want)
			}
			if c.line != "" && got[0].Line != c.line {
				t.Errorf("line = %q, want %q", got[0].Line, c.line)
			}
			if got[0].Fix == "" || got[0].Why == "" {
				t.Errorf("finding lacks cause or fix: %+v", got[0])
			}
		})
	}
}

// Every rule must be complete and its patterns must compile and be distinct.
func TestRulesAreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Rules {
		if r.ID == "" || r.Cause == "" || r.Fix == "" || (len(r.Patterns) == 0 && r.ExitCode == 0) {
			t.Errorf("incomplete rule %+v", r)
		}
		if seen[r.ID] {
			t.Errorf("duplicate rule %q", r.ID)
		}
		seen[r.ID] = true
		if strings.ContainsAny(r.ID, " _") {
			t.Errorf("rule ID %q must be lowercase words joined by '-'", r.ID)
		}
	}
}
