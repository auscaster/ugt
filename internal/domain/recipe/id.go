package recipe

import (
	"fmt"
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

// Path is the recipe's folder in the catalogue, relative to recipes/.
func (id ID) Path() string { return id.String() }
