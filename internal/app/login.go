package app

import (
	"context"
	"errors"

	"github.com/auscaster/ugt/internal/domain/ledger"
)

// The user's GitHub login is kept in the system's secret store under this
// name, beside the recipes' secrets, and uninstall removes it with them.
const (
	LoginRecipe = "ugt"
	LoginName   = "github"
)

// LoginResult is who signed in.
type LoginResult struct {
	User string `json:"user"`
}

// Login signs the user in to GitHub with the device flow: show tells them the
// code and where to enter it, in their own browser, and the token goes
// straight to the system's secret store. An agent may pass on the code; it
// never sees the token.
func (a *App) Login(ctx context.Context, show func(DeviceLogin)) (LoginResult, error) {
	if a.Secrets == nil {
		return LoginResult{}, errors.New("this machine has no secret store UGT can use; set GITHUB_TOKEN instead")
	}
	l, err := a.GitHub.StartLogin(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	show(l)
	token, user, err := a.GitHub.FinishLogin(ctx, l)
	if err != nil {
		return LoginResult{}, err
	}
	if err := a.storeSecret(ctx, LoginRecipe, LoginName, token); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{User: user}, nil
}

// Logout forgets the user's GitHub login.
func (a *App) Logout(ctx context.Context) error {
	if a.Secrets == nil {
		return nil
	}
	if err := a.Secrets.Delete(ctx, LoginRecipe, LoginName); err != nil {
		return err
	}
	_ = a.record(ledger.Entry{Kind: ledger.Secret, Name: LoginRecipe + "/" + LoginName, Recipe: LoginRecipe, Removed: a.Now()})
	return nil
}
