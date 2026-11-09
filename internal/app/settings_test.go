package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usegeekthing/ugt/internal/domain/catalogue"
	"github.com/usegeekthing/ugt/internal/domain/compose"
)

type memSecrets map[string]string

func (m memSecrets) Get(_ context.Context, recipe, name string) (string, bool, error) {
	v, ok := m[recipe+"/"+name]
	return v, ok, nil
}
func (m memSecrets) Set(_ context.Context, recipe, name, value string) error {
	m[recipe+"/"+name] = value
	return nil
}
func (m memSecrets) Delete(_ context.Context, recipe, name string) error {
	delete(m, recipe+"/"+name)
	return nil
}

type memValues map[string]map[string]string

func (m memValues) Get(recipe string) (map[string]string, error) { return m[recipe], nil }
func (m memValues) Set(recipe, name, value string) error {
	if m[recipe] == nil {
		m[recipe] = map[string]string{}
	}
	m[recipe][name] = value
	return nil
}
func (m memValues) Delete(recipe, name string) error { delete(m[recipe], name); return nil }

const exampleService = "../../spec/recipe/v0/examples/valid/example-service"

func TestSecretsReachTheRunAndNeverTheDisk(t *testing.T) {
	cfg := compose.Model{Services: map[string]compose.Service{"app": {}, "check": {Profiles: []string{"check"}}, "sample": {Profiles: []string{"sample"}}, "sample-check": {Profiles: []string{"sample"}}}}
	engine := &fakeEngine{config: cfg, services: map[string]func(string, io.Writer) int{"check": func(string, io.Writer) int { return 0 }}}
	a, _ := newApp(t, engine)
	secrets := memSecrets{}
	a.Secrets, a.Values = secrets, memValues{}
	a.Catalogues = []Catalogue{&fakeCatalogue{source: catalogue.Local, dirs: map[string]string{"example/service/dashboard": exampleService}}}
	ctx := context.Background()

	_, err := a.Run(ctx, RunOptions{Project: "example/service"})
	if err == nil || !strings.Contains(err.Error(), "ugt settings set example/service/dashboard PROVIDER_API_KEY") {
		t.Fatalf("a missing secret must name the command that sets it: %v", err)
	}
	if _, err := a.Run(ctx, RunOptions{Project: "example/service", Values: map[string]string{"PROVIDER_API_KEY": "sk-1"}}); err == nil || !strings.Contains(err.Error(), "is a secret") {
		t.Fatalf("a secret was accepted as a value: %v", err)
	}

	if _, err := a.SetSetting(ctx, "example/service/dashboard", "PROVIDER_API_KEY", "sk-secret-value"); err != nil {
		t.Fatal(err)
	}
	_, states, err := a.Settings(ctx, "example/service")
	if err != nil || len(states) != 1 || !states[0].Set || states[0].Value != "" {
		t.Fatalf("settings %+v, %v", states, err)
	}
	res, err := a.Run(ctx, RunOptions{Project: "example/service", Allow: true})
	if err != nil || !res.Passed || len(res.Pages) != 1 || len(res.Endpoints) != 1 {
		t.Fatalf("run %+v, %v", res, err)
	}
	// the page and the endpoint share their port, published once, at an
	// address that stays the same from run to run
	if res.Pages[0].URL != "http://127.0.0.1:50001/" || res.Endpoints[0].URL != "http://127.0.0.1:50001/api" {
		t.Errorf("addresses %+v %+v", res.Pages, res.Endpoints)
	}
	// even when the earlier run's port comes free only a moment after it
	// stops, as the forwards of UGT's Mac machine and of WSL do
	lingering, free := 2, a.FreePort
	a.FreePort = func(want int) (int, error) {
		if want == 50001 && lingering > 0 {
			lingering--
			return free(0)
		}
		return free(want)
	}
	// and the address is handed over only once it answers on this computer
	checks := 0
	a.Listening = func(port int) bool { checks++; return checks > 2 }
	again, err := a.Run(ctx, RunOptions{Project: "example/service", Allow: true})
	if err != nil || again.Pages[0].URL != res.Pages[0].URL {
		t.Errorf("the page moved from %s to %+v: %v", res.Pages[0].URL, again.Pages, err)
	}
	if checks != 3 {
		t.Errorf("the address was checked %d times before it was handed over", checks)
	}
	if engine.lastEnv["PROVIDER_API_KEY"] != "sk-secret-value" {
		t.Errorf("the secret did not reach the engine")
	}
	session, _ := os.ReadFile(filepath.Join(a.Layout.Workspaces, "example", "service", "dashboard", sessionFile))
	if strings.Contains(string(session), "sk-secret-value") || len(session) == 0 {
		t.Errorf("the secret reached the disk: %s", session)
	}
	if _, err := a.ForgetSetting(ctx, "example/service/dashboard", "PROVIDER_API_KEY"); err != nil || len(secrets) != 0 {
		t.Errorf("forget: %v, %v", err, secrets)
	}
}
