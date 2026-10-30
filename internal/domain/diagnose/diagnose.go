// Package diagnose turns a failed recipe step into a plain cause and the exact
// fix, from known pitfalls kept as data. Verification reports carry its
// findings, and the skill's pitfalls reference is generated from the same
// rules, so agents and people learn one thing once.
package diagnose

import (
	"regexp"
	"strings"
)

// Rule is one known pitfall.
type Rule struct {
	ID       string   // stable identifier, e.g. "memory"
	ExitCode int      // matches this exit code too, when non-zero
	Patterns []string // regular expressions matched against the step's log
	Cause    string   // what went wrong, in plain words
	Fix      string   // what to change in the recipe, exactly
	Seen     []string // recipes where it happened, as evidence
}

// Finding is a rule that matched a failed step.
type Finding struct {
	Rule string `json:"rule"`
	Line string `json:"line,omitempty"` // the log line that matched, if any
	Why  string `json:"cause"`
	Fix  string `json:"fix"`
}

// Rules are the known pitfalls, most specific first.
var Rules = []Rule{
	{
		ID:       "memory",
		ExitCode: 137,
		Patterns: []string{`OOMKilled`, `(?i)out of memory`, `(?m)^Killed$`},
		Cause:    "The container ran out of memory and was killed (exit 137).",
		Fix:      "Raise the service's mem_limit, or make it use less: fewer workers, a smaller model. Run ugt doctor to see how much memory the engine has.",
		Seen:     []string{"danielgatis/rembg", "firecrawl/firecrawl/markdown"},
	},
	{
		ID:       "architecture",
		Patterns: []string{`(?i)failed to open elf`, `(?i)exec format error`, `(?i)rosetta error`},
		Cause:    "A binary built for another CPU architecture ran in the image.",
		Fix:      "Build with platform: linux/${UGT_ARCH} and pass TARGETARCH: ${UGT_ARCH} as a build arg. An upstream ARG TARGETARCH default wins even over --platform.",
		Seen:     []string{"heygen-com/hyperframes/video"},
	},
	{
		ID:       "opengl",
		Patterns: []string{`libGL\.so\.1`},
		Cause:    "A graphics library the program loads at start is missing from the image.",
		Fix:      "apt-get install -y --no-install-recommends libgl1 libglib2.0-0 in the Dockerfile. Slim images leave them out.",
		Seen:     []string{"earthtojake/text-to-cad/stl"},
	},
	{
		ID:       "dns",
		Patterns: []string{`Temporary failure in name resolution`, `Could not resolve host`, `getaddrinfo (ENOTFOUND|EAI_AGAIN)`, `Name or service not known`},
		Cause:    "The container could not resolve a host name.",
		Fix:      "Set dns: [1.1.1.1, 8.8.8.8] on the service that fetches from the internet. Some engine VMs do not pass the host's resolver through.",
		Seen:     []string{"firecrawl/firecrawl/markdown"},
	},
	{
		ID:       "no-network",
		Patterns: []string{`(?i)network is unreachable`},
		Cause:    "A service with network_mode: none tried to reach the network.",
		Fix:      "Remove network_mode: none from the service that needs the network, and keep it on every service that does not.",
	},
	{
		ID:       "healthcheck",
		Patterns: []string{`has no healthcheck configured`},
		Cause:    "A service waits for a dependency to be healthy, but the dependency defines no healthcheck.",
		Fix:      "Add a healthcheck to the dependency in compose.yaml, for example an HTTP request to its port, or wait for service_started instead.",
		Seen:     []string{"firecrawl/firecrawl/markdown"},
	},
	{
		ID:       "variable",
		Patterns: []string{`variable is not set`, `required variable \S+ is missing`},
		Cause:    "compose.yaml reads a variable nobody set.",
		Fix:      "Declare it as a setting in recipe.json so the user supplies it, or write the value in compose.yaml.",
	},
	{
		ID:       "image",
		Patterns: []string{`pull access denied`, `manifest unknown`, `repository does not exist`, `No such image`},
		Cause:    "An image name or tag does not exist.",
		Fix:      "Built images must be named ${UGT_IMAGE} (or ${UGT_IMAGE}-<suffix>); published images must be pinned by an existing digest.",
	},
	{
		ID:       "port",
		Patterns: []string{`port is already allocated`, `(?i)address already in use`},
		Cause:    "A published host port is already taken.",
		Fix:      "Publish as 127.0.0.1::<port> so the engine picks a free host port; ugt reports the one it chose.",
	},
	{
		ID:       "read-only",
		Patterns: []string{`Read-only file system`, `EROFS`},
		Cause:    "The program writes into a folder mounted read-only.",
		Fix:      "Mount that folder writable, or point the program's cache or state directory at /tmp.",
		Seen:     []string{"heygen-com/hyperframes/video"},
	},
	{
		ID:       "permission",
		Patterns: []string{`(?i)permission denied`},
		Cause:    "The container's user cannot read or write a mounted path.",
		Fix:      "Write outputs as a user that can: run that service as root, or create the output folder in the image with the right owner.",
	},
	{
		ID:       "python-module",
		Patterns: []string{`ModuleNotFoundError: No module named '[^']+'`},
		Cause:    "A Python package the program imports is not installed in the image.",
		Fix:      "Install it in the Dockerfile, pinned, in the same environment the program runs with.",
		Seen:     []string{"earthtojake/text-to-cad/stl"},
	},
	{
		ID:       "disk",
		Patterns: []string{`no space left on device`},
		Cause:    "The engine ran out of disk space.",
		Fix:      "Free space or give the engine more; ugt doctor shows where the space went.",
	},
	{
		ID:       "engine",
		Patterns: []string{`Cannot connect to the Docker daemon`, `error during connect`},
		Cause:    "The container engine is not running.",
		Fix:      "Run ugt doctor; it names the engine and how to start it.",
	},
}

var compiled = func() [][]*regexp.Regexp {
	out := make([][]*regexp.Regexp, len(Rules))
	for i, r := range Rules {
		for _, p := range r.Patterns {
			out[i] = append(out[i], regexp.MustCompile(p))
		}
	}
	return out
}()

// Diagnose returns the rules that match a failed step's exit code and log,
// each once, in rule order.
func Diagnose(exitCode int, log string) []Finding {
	var findings []Finding
	for i, r := range Rules {
		if line, ok := match(compiled[i], log); ok {
			findings = append(findings, Finding{Rule: r.ID, Line: line, Why: r.Cause, Fix: r.Fix})
		} else if r.ExitCode != 0 && r.ExitCode == exitCode {
			findings = append(findings, Finding{Rule: r.ID, Why: r.Cause, Fix: r.Fix})
		}
	}
	return findings
}

func match(patterns []*regexp.Regexp, log string) (string, bool) {
	for _, re := range patterns {
		if loc := re.FindStringIndex(log); loc != nil {
			start := strings.LastIndexByte(log[:loc[0]], '\n') + 1
			end := loc[1] + strings.IndexByte(log[loc[1]:]+"\n", '\n')
			return strings.TrimSpace(log[start:end]), true
		}
	}
	return "", false
}
