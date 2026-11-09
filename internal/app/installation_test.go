package app

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/usegeekthing/ugt/internal/domain/catalogue"
	"github.com/usegeekthing/ugt/internal/domain/compose"
	"github.com/usegeekthing/ugt/internal/domain/recipe"
)

func TestNamedServicesShareOnlyTheBuiltImage(t *testing.T) {
	ctx := context.Background()
	const id = "example/service/dashboard"
	engine := &fakeEngine{config: compose.Model{Services: map[string]compose.Service{"app": {Build: &compose.Build{}}, "check": {Profiles: []string{"check"}}}}, services: map[string]func(string, io.Writer) int{"check": func(string, io.Writer) int { return 0 }}}
	a, _ := newApp(t, engine)
	a.Secrets, a.Values = memSecrets{}, memValues{}
	a.Catalogues = []Catalogue{&fakeCatalogue{source: catalogue.Local, dirs: map[string]string{id: exampleService}}}
	for _, name := range []string{"test", "production"} {
		target := id + "@" + name
		if _, err := a.SetSetting(ctx, target, "PROVIDER_API_KEY", name+"-secret"); err != nil {
			t.Fatal(err)
		}
		res, err := a.Run(ctx, RunOptions{Project: "example/service", Instance: name, Allow: true})
		if err != nil || !res.Passed || res.Recipe != id || res.Installation != target || engine.lastEnv["PROVIDER_API_KEY"] != name+"-secret" {
			t.Fatalf("run %s: %+v, %v", name, res, err)
		}
	}
	if _, err := a.Stop(ctx, "example/service"); err == nil {
		t.Fatal("an unqualified project stopped a named copy")
	}
	if _, err := a.Remove(ctx, id+"@test"); err != nil {
		t.Fatal(err)
	}
	if len(engine.images) != 1 {
		t.Fatal("removed the other installation's image")
	}
	if _, err := a.Remove(ctx, id+"@production"); err != nil {
		t.Fatal(err)
	}
	if len(engine.images) != 0 {
		t.Fatal("left the last installation's image")
	}
}

func TestNamedInstallationBoundaries(t *testing.T) {
	id, _ := recipe.ParseID("example/service/dashboard")
	for _, name := range []string{"../outside", "other@task", "Test", "two--words", strings.Repeat("x", 33)} {
		if _, err := id.Installation(name); err == nil {
			t.Errorf("accepted unsafe name %q", name)
		}
	}
	one, _ := id.Installation("test")
	if one.Project() == id.Project() {
		t.Fatal("shared default Compose project")
	}
	for _, svc := range []compose.Service{
		{Ports: []compose.Port{{Published: "1234"}}},
		{Mounts: []compose.Mount{{Type: "bind", Source: "/shared/data", Target: "/data"}}},
		{Mounts: []compose.Mount{{Type: "volume", Source: "db", Target: "/data"}}},
	} {
		m := compose.Model{Project: one.Project(), Services: map[string]compose.Service{"app": svc}, Volumes: map[string]compose.Volume{"db": {Name: "shared-data", External: true}}}
		if err := checkInstallation(m, "/workspace"); err == nil {
			t.Errorf("accepted shared resource: %+v", svc)
		}
	}
}

func TestNamedCopiesRejectSettingsBakedIntoTheSharedImage(t *testing.T) {
	f := recipe.Folder{Recipe: recipe.Recipe{ID: "example/pages/serve", Kind: recipe.KindService, Settings: []recipe.Setting{{Name: "BANNER", Kind: "value"}}}, Compose: []byte("services:\n  app:\n    build:\n      args:\n        TEXT: ${BANNER:-hello}\n")}
	if err := f.NamedService(); err == nil || !strings.Contains(err.Error(), "BANNER") {
		t.Fatalf("could serve another copy's banner: %v", err)
	}
	f.Compose = []byte("services:\n  app:\n    build: ./recipe\n    environment:\n      TEXT: ${BANNER:-hello}\n")
	if err := f.NamedService(); err != nil {
		t.Fatal(err)
	}
}
