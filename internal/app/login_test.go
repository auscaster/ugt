package app

import (
	"context"
	"testing"

	"github.com/auscaster/ugt/internal/domain/ledger"
)

func TestLoginKeepsTheTokenInTheSecretStore(t *testing.T) {
	a, led := newApp(t, &fakeEngine{})
	secrets := memSecrets{}
	gh := &fakeGitHub{approve: true}
	a.Secrets, a.GitHub = secrets, gh
	var shown DeviceLogin
	res, err := a.Login(context.Background(), func(l DeviceLogin) { shown = l })
	if err != nil || res.User != "someone" || shown.Code != "WDJB-MJHT" {
		t.Fatalf("login %+v, %v, shown %+v", res, err, shown)
	}
	if v, ok, _ := secrets.Get(context.Background(), LoginRecipe, LoginName); !ok || v != "gho_user" {
		t.Errorf("token %q", v)
	}
	if live := ledger.Live(led.entries); len(live) != 1 || live[0].Kind != ledger.Secret {
		t.Errorf("ledger %+v", live)
	}
	if err := a.Logout(context.Background()); err != nil || len(secrets) != 0 || len(ledger.Live(led.entries)) != 0 {
		t.Errorf("logout: %v, %v", err, secrets)
	}
	gh.approve = false
	if _, err := a.Login(context.Background(), func(DeviceLogin) {}); err == nil || len(secrets) != 0 {
		t.Errorf("declined: %v, %v", err, secrets)
	}
}
