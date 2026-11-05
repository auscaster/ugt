package recipe

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
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

// NamedService checks that multiple copies can use the same verified images.
func (f *Folder) NamedService() error {
	r := f.Recipe
	if r.Kind != KindService || len(r.Provides.Commands)+len(r.Provides.MCPServers) > 0 {
		return fmt.Errorf("named copies require a service without machine-wide commands or MCP tools")
	}
	if len(r.Settings) == 0 {
		return nil
	}
	var config struct {
		Services map[string]struct{ Build any }
	}
	if err := yaml.Unmarshal(f.Compose, &config); err != nil {
		return err
	}
	for _, name := range sortedKeys(config.Services) {
		for _, variable := range ComposeVariables(config.Services[name].Build) {
			if !executorVars[variable.Name] {
				return fmt.Errorf("%s bakes %s into the image for services.%s; independent copies share a verified image. Pass this setting to the running service, or make a separate recipe for this build", r.ID, variable.Name, name)
			}
		}
	}
	return nil
}
