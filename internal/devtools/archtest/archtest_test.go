package archtest

import (
	"strings"
	"testing"
)

// The whole module must keep the import rules.
func TestModuleKeepsTheLayers(t *testing.T) {
	problems, err := Check("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

func TestRules(t *testing.T) {
	m := Module + "/"
	cases := []struct {
		pkg, imp, want string
	}{
		{m + "internal/domain/recipe", "encoding/json", ""},
		{m + "internal/domain/recipe", "gopkg.in/yaml.v3", ""},
		{m + "internal/domain/recipe", "github.com/some/http-client", "only the standard library"},
		{m + "internal/domain/recipe", m + "internal/app", "only the domain"},
		{m + "internal/domain/recipe", "os", "only through app's ports"},
		{m + "internal/domain/recipe", "io/fs", ""},
		{m + "internal/app", "os/exec", "only through app's ports"},
		{m + "internal/app", "net/url", ""},
		{m + "internal/adapter/host", "os/exec", ""},
		{m + "internal/app", m + "internal/domain/recipe", ""},
		{m + "internal/app", m + "internal/adapter/docker", "only the domain"},
		{m + "internal/adapter/docker", m + "internal/app", ""},
		{m + "internal/adapter/docker", m + "internal/adapter/wsl", "another adapter"},
		{m + "internal/adapter/keychain/darwin", m + "internal/adapter/keychain", ""},
		{m + "internal/adapter/docker", m + "internal/surface/cli", "never import surfaces"},
		{m + "internal/surface/cli", m + "internal/adapter/docker", "through app's ports"},
		{m + "internal/surface/mcp", m + "internal/app", ""},
		{m + "cmd/ugt", m + "internal/adapter/docker", ""},
		{m + "cmd/ugt", m + "internal/devtools/readme", "nothing shipped"},
		{m + "spec", m + "internal/app", "imports nothing internal"},
	}
	for _, c := range cases {
		got := rule(c.pkg, Layer(c.pkg), c.imp)
		if got == "" {
			got = worldRule(Layer(c.pkg), c.imp)
		}
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s -> %s: got %q, want %q", c.pkg, c.imp, got, c.want)
		}
	}
}
