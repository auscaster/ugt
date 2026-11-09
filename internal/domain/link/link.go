// Package link is a link to UGT from a web page: ugt://run/<owner>/<repo>, or
// ugt://run/<owner>/<repo>/<task> for one of its recipes. It opens UGT's own
// page for a catalogue recipe, which says what it will run and with what
// access, and runs nothing until the user starts it there. A link names a
// recipe and nothing else: no files, settings or commands, as any web page
// can open one.
package link

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/usegeekthing/ugt/internal/domain/recipe"
)

// Scheme is the URL scheme UGT registers with the operating system.
const Scheme = "ugt"

// Link is a project, and a task when the link names one.
type Link struct {
	Project string // owner/repo, lowercase
	Task    string // "" for the project's recipes
}

// Parse reads a ugt:// link.
func Parse(s string) (Link, error) {
	bad := fmt.Errorf("%q is not a link UGT opens: it opens ugt://run/<owner>/<repo>[/<task>]", s)
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme != Scheme || u.Host != "run" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Link{}, bad
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch len(parts) {
	case 2:
		owner, repo, err := recipe.ParseProject(parts[0] + "/" + parts[1])
		if err != nil {
			return Link{}, bad
		}
		return Link{Project: strings.ToLower(owner + "/" + repo)}, nil
	case 3:
		id, err := recipe.ParseID(strings.ToLower(strings.Join(parts, "/")))
		if err != nil {
			return Link{}, bad
		}
		return Link{Project: id.Owner + "/" + id.Repo, Task: id.Task}, nil
	}
	return Link{}, bad
}

// String is the link, as a web page writes it.
func (l Link) String() string {
	s := Scheme + "://run/" + l.Project
	if l.Task != "" {
		s += "/" + l.Task
	}
	return s
}
