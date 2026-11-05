package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/surface/cli"
)

// Exercise the installed settings command with the runner account's real
// credential store. A temporary HOME does not isolate OS credentials.
func TestNamedServiceCredentials(t *testing.T) {
	if os.Getenv("UGT_KEYCHAIN_TEST") != "1" {
		t.Skip("native credential qualification is opt-in on hosted runners")
	}
	if os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" || os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Fatal("native credential qualification requires a disposable GitHub-hosted runner")
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("this workflow provisions a native credential store on macOS and Windows")
	}
	const base = "example/service/dashboard"
	const key = "PROVIDER_API_KEY"
	dir := t.TempDir()
	if err := os.CopyFS(filepath.Join(dir, filepath.FromSlash(base)), os.DirFS("../../spec/recipe/v0/examples/valid/example-service")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UGT_CATALOGUE", dir)
	a, err := wire()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	nonce := strings.ToLower(rand.Text()[:12])
	trial, prod := base+"@trial-"+nonce, base+"@prod-"+nonce
	trialSecret, prodSecret := rand.Text(), rand.Text()+` "quoted" \ path`
	command := func(input string, args ...string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		c := cli.CLI{App: a, Stdin: strings.NewReader(input + "\n"), Stdout: &out, Stderr: &stderr}
		if code := c.Run(ctx, args); code != 0 {
			t.Fatalf("%s failed with exit %d", args[1], code)
		}
		return out.String()
	}
	expect := func(target, want string) {
		t.Helper()
		value, ok, err := a.Secrets.Get(ctx, target, key)
		if err != nil || ok != (want != "") || value != want {
			t.Fatalf("credential readback disagrees for %s", target)
		}
		var state struct {
			Recipe   string
			Settings []app.SettingState
		}
		output := command("", "settings", "list", "--json", target)
		if err := json.Unmarshal([]byte(output), &state); err != nil {
			t.Fatal(err)
		}
		if state.Recipe != target || len(state.Settings) != 1 || state.Settings[0].Set != ok || state.Settings[0].Value != "" {
			t.Fatal("incorrect or unredacted settings listing")
		}
	}
	for _, tc := range []struct{ target, value string }{{trial, trialSecret}, {prod, prodSecret}} {
		expect(tc.target, "")
		command(tc.value, "settings", "set", tc.target, key)
		t.Cleanup(func() {
			if err := a.Secrets.Delete(ctx, tc.target, key); err != nil {
				t.Errorf("remove temporary credential: %v", err)
			}
		})
		expect(tc.target, tc.value)
	}
	if runtime.GOOS == "darwin" {
		t.Run("alternate home cannot use an existing credential", func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			isolated, err := wire()
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := isolated.Secrets.Get(ctx, trial, key); err == nil {
				t.Fatal("alternate HOME could read the credential")
			}
			if err := isolated.Secrets.Set(ctx, trial, key, "unwanted replacement"); err == nil {
				t.Fatal("alternate HOME could replace the credential")
			}
			if err := isolated.Secrets.Delete(ctx, trial, key); err == nil {
				t.Fatal("alternate HOME could delete the credential")
			}
		})
		expect(trial, trialSecret)
	}
	command(prodSecret+" updated", "settings", "set", prod, key)
	command("", "settings", "forget", trial, key)
	expect(trial, "")
	expect(prod, prodSecret+" updated")
	command("", "settings", "forget", prod, key)
	expect(prod, "")
}
