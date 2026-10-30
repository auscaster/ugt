package recipe

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	idPart   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,99}$`)
	taskSlug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	nonName  = regexp.MustCompile(`[^a-z0-9-]+`)
)

// ID names a recipe: the project (owner/repo) and the task, as owner/repo/task.
type ID struct {
	Owner, Repo, Task string
}

// ParseID parses "owner/repo/task".
func ParseID(s string) (ID, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 3 {
		return ID{}, fmt.Errorf("recipe ID %q is not owner/repo/task", s)
	}
	id := ID{Owner: parts[0], Repo: parts[1], Task: parts[2]}
	if !idPart.MatchString(id.Owner) || !idPart.MatchString(id.Repo) {
		return ID{}, fmt.Errorf("recipe ID %q: owner and repo are lowercase letters, digits, '.', '_' and '-'", s)
	}
	if !taskSlug.MatchString(id.Task) {
		return ID{}, fmt.Errorf("recipe ID %q: the task is lowercase words joined by '-'", s)
	}
	return id, nil
}

func (id ID) String() string { return id.Owner + "/" + id.Repo + "/" + id.Task }

// Project is the Compose project name: ugt-<owner>-<repo>-<task>, with every
// character other than a-z, 0-9 and '-' replaced by '-'.
func (id ID) Project() string {
	return "ugt-" + nonName.ReplaceAllString(id.Owner+"-"+id.Repo+"-"+id.Task, "-")
}

// Image is the local image name for a recipe revision: ugt/<owner>/<repo>/<task>:<rev>.
// Compose recipes that build several services tag the others <image>-<suffix>.
func (id ID) Image(rev string) string {
	return "ugt/" + id.Owner + "/" + id.Repo + "/" + id.Task + ":" + rev
}

// ParseProject reads a GitHub project as owner/repo, github.com/owner/repo or
// a link into it, such as https://github.com/owner/repo/tree/main/docs.
// Case is kept, for GitHub; IDs lowercase it.
func ParseProject(s string) (owner, repo string, err error) {
	bad := fmt.Errorf("%q is not a GitHub project: give owner/repo or its github.com link", s)
	in := strings.TrimSpace(s)
	var parts []string
	if strings.Contains(in, "://") || strings.HasPrefix(in, "github.com/") || strings.HasPrefix(in, "www.github.com/") {
		if !strings.Contains(in, "://") {
			in = "https://" + in
		}
		u, err := url.Parse(in)
		if err != nil || u.Host != "github.com" && u.Host != "www.github.com" {
			return "", "", bad
		}
		if parts = strings.Split(strings.Trim(u.Path, "/"), "/"); len(parts) < 2 {
			return "", "", bad
		}
	} else if parts = strings.Split(in, "/"); len(parts) != 2 {
		return "", "", bad
	}
	owner, repo = parts[0], strings.TrimSuffix(parts[1], ".git")
	if !idPart.MatchString(strings.ToLower(owner)) || !idPart.MatchString(strings.ToLower(repo)) {
		return "", "", bad
	}
	return owner, repo, nil
}

// Path is the recipe's folder in the catalogue, relative to recipes/.
func (id ID) Path() string { return id.String() }
