package recipe

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// SubmitFileLimit bounds one file in a submitted recipe: recipes hold
// configuration, scripts and small samples, never data.
const SubmitFileLimit = 5 << 20

// Leak is something personal found in a recipe that is about to be shared.
type Leak struct {
	File string `json:"file"`
	Line int    `json:"line,omitempty"`
	What string `json:"what"`
}

func (l Leak) String() string {
	if l.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", l.File, l.Line, l.What)
	}
	return l.File + ": " + l.What
}

// Private is what must never leave the machine in a shared recipe.
type Private struct {
	Paths   []string          // the user's own folders: home, UGT's state, the UGT folder
	Secrets []string          // the values of the user's secrets
	Inputs  map[string]string // SHA-256 of each file the user ran the recipe on, to its name
}

// keyFormats are the shapes of common credentials, whoever's they are.
var keyFormats = []struct {
	what string
	re   *regexp.Regexp
}{
	{"a GitHub token", regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36}\b|\bgithub_pat_[A-Za-z0-9_]{22,}`)},
	{"an API key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
	{"an AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"a Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{"a private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
}

// Scrub finds what must not be shared in a recipe's files, by path: the
// user's paths and secrets, credentials of any kind, copies of the user's
// inputs, and files too large for a recipe. Nothing is changed; each finding
// says where it is, for the author to remove.
func Scrub(files map[string][]byte, p Private) []Leak {
	var leaks []Leak
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := files[name]
		if len(data) > SubmitFileLimit {
			leaks = append(leaks, Leak{File: name, What: fmt.Sprintf("is %d MB; recipes hold configuration, scripts and small samples", len(data)>>20)})
		}
		sum := sha256.Sum256(data)
		if input, ok := p.Inputs[fmt.Sprintf("%x", sum)]; ok {
			leaks = append(leaks, Leak{File: name, What: "is a copy of " + input + ", a file you ran the recipe on"})
			continue
		}
		if bytes.IndexByte(data, 0) >= 0 {
			continue // binary: a sample image or recording, checked above
		}
		for i, line := range strings.Split(string(data), "\n") {
			if path := personalPath(line, p.Paths); path != "" {
				leaks = append(leaks, Leak{File: name, Line: i + 1, What: "names a folder on this machine (" + path + "): recipes use workspace paths"})
			}
			for _, s := range p.Secrets {
				if len(s) >= 6 && strings.Contains(line, s) {
					leaks = append(leaks, Leak{File: name, Line: i + 1, What: "holds the value of one of your secrets: declare it as a setting instead"})
				}
			}
			for _, k := range keyFormats {
				if k.re.MatchString(line) {
					leaks = append(leaks, Leak{File: name, Line: i + 1, What: "looks like " + k.what + ": declare it as a secret setting instead"})
				}
			}
		}
	}
	return dedupeLeaks(leaks)
}

// personalPath returns the first of paths that line names, in any form.
func personalPath(line string, paths []string) string {
	lower := strings.ToLower(line)
	for _, path := range paths {
		for _, form := range pathForms(path) {
			if form != "" && strings.Contains(lower, strings.ToLower(form)) {
				return path
			}
		}
	}
	return ""
}

// pathForms are the ways a path can be written: as given, with forward
// slashes, with backslashes, and with backslashes escaped as JSON and YAML
// strings write them.
func pathForms(p string) []string {
	back := strings.ReplaceAll(p, "/", `\`)
	return []string{p, strings.ReplaceAll(p, `\`, "/"), back, strings.ReplaceAll(back, `\`, `\\`)}
}

func dedupeLeaks(leaks []Leak) []Leak {
	seen := map[string]bool{}
	out := leaks[:0]
	for _, l := range leaks {
		if key := l.String(); !seen[key] {
			seen[key] = true
			out = append(out, l)
		}
	}
	return out
}
