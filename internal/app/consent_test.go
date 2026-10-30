package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/compose"
)

// askingPrompt answers as the user would, and records what it was asked.
type askingPrompt struct {
	agree     bool
	secret    string
	noDesktop bool
	asked     []Question
}

func (p *askingPrompt) Confirm(_ context.Context, q Question) (bool, error) {
	p.asked = append(p.asked, q)
	if p.noDesktop {
		return false, ErrNoPrompt
	}
	return p.agree, nil
}

func (p *askingPrompt) Secret(_ context.Context, q Question) (string, error) {
	p.asked = append(p.asked, q)
	if p.noDesktop {
		return "", ErrNoPrompt
	}
	return p.secret, nil
}

type memConsents map[string][]string

func (m memConsents) Agreed(recipe, digest string) (bool, error) {
	for _, d := range m[recipe] {
		if d == digest {
			return true, nil
		}
	}
	return false, nil
}
func (m memConsents) Agree(recipe, digest string) error {
	m[recipe] = append(m[recipe], digest)
	return nil
}

// the example service: its app publishes a local port, so it reaches the
// internet, which its recipe declares
func serviceApp(t *testing.T, p Prompt) (*App, *fakeEngine) {
	t.Helper()
	cfg := compose.Model{Services: map[string]compose.Service{
		"app": {Ports: []compose.Port{{Target: 8080, HostIP: "127.0.0.1"}}}, "check": {Profiles: []string{"check"}, NetworkMode: "none"},
		"sample": {Profiles: []string{"sample"}, NetworkMode: "none"}, "sample-check": {Profiles: []string{"sample"}, NetworkMode: "none"},
	}}
	engine := &fakeEngine{config: cfg, services: map[string]func(string, io.Writer) int{"check": func(string, io.Writer) int { return 0 }}}
	a, _ := newApp(t, engine)
	a.Secrets, a.Values, a.Consents, a.Prompt = memSecrets{}, memValues{}, memConsents{}, p
	a.Catalogues = []Catalogue{&fakeCatalogue{source: catalogue.Local, dirs: map[string]string{"example/service/dashboard": exampleService}}}
	return a, engine
}

// UGT asks the user itself, once, and again only when the access changes.
func TestRunAsksForAccessOnce(t *testing.T) {
	p := &askingPrompt{agree: true, secret: "sk-typed"}
	a, engine := serviceApp(t, p)
	ctx := context.Background()
	if _, err := a.Run(ctx, RunOptions{Project: "example/service"}); err != nil {
		t.Fatal(err)
	}
	// the missing secret, then the access
	if len(p.asked) != 2 || !strings.Contains(p.asked[0].Message, "PROVIDER_API_KEY") || !strings.Contains(p.asked[1].Message, "Reach the internet") ||
		!strings.Contains(p.asked[1].Message, "services.app reaches the internet") || !strings.Contains(p.asked[1].Message, "The recipe says: ") {
		t.Fatalf("asked %+v", p.asked)
	}
	if engine.lastEnv["PROVIDER_API_KEY"] != "sk-typed" {
		t.Error("the typed secret did not reach the run")
	}
	if _, err := a.Run(ctx, RunOptions{Project: "example/service"}); err != nil || len(p.asked) != 2 {
		t.Fatalf("asked again: %v, %d questions", err, len(p.asked))
	}
	app := engine.config.Services["app"]
	app.ExtraHosts = []string{"ollama=host-gateway"}
	engine.config.Services["app"] = app
	// a grant the recipe does not declare is refused before anyone is asked
	if _, err := a.Run(ctx, RunOptions{Project: "example/service"}); err == nil || !strings.Contains(err.Error(), `"kind": "host-services"`) || len(p.asked) != 2 {
		t.Fatalf("undeclared: %v, %d questions", err, len(p.asked))
	}
}

func TestDeclinedAccessRunsNothing(t *testing.T) {
	p := &askingPrompt{agree: false}
	a, engine := serviceApp(t, p)
	if _, err := a.SetSetting(context.Background(), "example/service/dashboard", "PROVIDER_API_KEY", "sk-1"); err != nil {
		t.Fatal(err)
	}
	_, err := a.Run(context.Background(), RunOptions{Project: "example/service"})
	if !errors.Is(err, ErrDeclined) || len(engine.ran) != 0 {
		t.Fatalf("got %v, ran %v", err, engine.ran)
	}
}

// Where nothing can ask the user, the run says how they agree ahead of time,
// and ugt allow records it.
func TestAllowAheadOfTheRun(t *testing.T) {
	p := &askingPrompt{noDesktop: true}
	a, _ := serviceApp(t, p)
	ctx := context.Background()
	if _, err := a.SetSetting(ctx, "example/service/dashboard", "PROVIDER_API_KEY", "sk-1"); err != nil {
		t.Fatal(err)
	}
	var needs *NeedsAgreement
	if _, err := a.Run(ctx, RunOptions{Project: "example/service"}); !errors.As(err, &needs) || !strings.Contains(err.Error(), "ugt allow example/service/dashboard") {
		t.Fatalf("got %v", err)
	}
	res, err := a.Allow(ctx, "example/service/dashboard", true)
	if err != nil || !res.Allowed || len(res.Grants) != 1 || res.Grants[0].Kind != "internet" {
		t.Fatalf("allow %+v, %v", res, err)
	}
	if _, err := a.Run(ctx, RunOptions{Project: "example/service"}); err != nil {
		t.Fatalf("after allow: %v", err)
	}
}
