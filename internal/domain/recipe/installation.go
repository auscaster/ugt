package recipe

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Installation names a local copy. Only ID identifies the upstream recipe.
type Installation struct {
	ID   ID
	Name string
}

func (id ID) Installation(name string) (Installation, error) {
	if name != "" && (len(name) > 32 || !taskSlug.MatchString(name)) {
		return Installation{}, fmt.Errorf("choose an installation name of up to 32 lowercase letters or digits, with single hyphens between words")
	}
	return Installation{ID: id, Name: name}, nil
}

func ParseInstallation(target string) (Installation, error) {
	base, name, named := strings.Cut(target, "@")
	if named && name == "" {
		return Installation{}, fmt.Errorf("give the installation name after @")
	}
	id, err := ParseID(base)
	if err != nil {
		return Installation{}, err
	}
	return id.Installation(name)
}

func (i Installation) String() string {
	if i.Name == "" {
		return i.ID.String()
	}
	return i.ID.String() + "@" + i.Name
}

func (i Installation) Project() string {
	if i.Name == "" {
		return i.ID.Project()
	}
	digest := sha256.Sum256([]byte(i.String()))
	return fmt.Sprintf("ugti-%s-%x", i.Name, digest[:12])
}

// NamedService checks the interfaces that a second installation would expose.
func (r Recipe) NamedService() error {
	if r.Kind != KindService || len(r.Provides.Commands)+len(r.Provides.MCPServers) > 0 {
		return fmt.Errorf("%s supports named copies only for services without machine-wide commands or MCP tools", r.ID)
	}
	return nil
}
