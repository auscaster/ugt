package prompt

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/app"
)

// fake answers every command with one exit, and records them.
func fake(goos string, exit int, stdout, stderr string, env map[string]string, installed ...string) (*Desktop, *[]command) {
	var ran []command
	return &Desktop{
		goos: goos,
		env:  func(k string) string { return env[k] },
		look: func(name string) (string, error) {
			if slices.Contains(installed, name) {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		run: func(_ context.Context, c command) (string, string, int, error) {
			ran = append(ran, c)
			return stdout, stderr, exit, nil
		},
	}, &ran
}

var q = app.Question{Title: "UGT", Message: `a/b/c asks "for" it; $(rm -rf ~)`}

func TestMacOS(t *testing.T) {
	d, ran := fake("darwin", 0, "", "", nil)
	if ok, err := d.Confirm(context.Background(), q); !ok || err != nil {
		t.Fatalf("confirm %v %v", ok, err)
	}
	c := (*ran)[0]
	// the message is an argument the script reads, never script text
	if c.name != "osascript" || !slices.Equal(c.args[len(c.args)-4:], []string{"UGT", q.Message, "Allow", "Don't allow"}) ||
		strings.Contains(strings.Join(c.args[:len(c.args)-4], " "), "rm -rf") {
		t.Errorf("command %+v", c)
	}
	d, _ = fake("darwin", 1, "", "execution error: User canceled. (-128)", nil)
	if ok, err := d.Confirm(context.Background(), q); ok || err != nil {
		t.Errorf("cancel: %v %v", ok, err)
	}
	d, _ = fake("darwin", 1, "", "execution error: No user interaction allowed. (-1713)", nil)
	if _, err := d.Confirm(context.Background(), q); !errors.Is(err, app.ErrNoPrompt) {
		t.Errorf("no desktop: %v", err)
	}
	d, _ = fake("darwin", 0, "hunter2\n", "", nil)
	if v, err := d.Secret(context.Background(), q); v != "hunter2" || err != nil {
		t.Errorf("secret %q %v", v, err)
	}
}

func TestWindows(t *testing.T) {
	d, ran := fake("windows", declined, "", "", nil)
	if ok, err := d.Confirm(context.Background(), q); ok || err != nil {
		t.Fatalf("declined: %v %v", ok, err)
	}
	c := (*ran)[0]
	if c.name != "powershell.exe" || !slices.Contains(c.env, "UGT_PROMPT_MESSAGE="+q.Message) || strings.Contains(strings.Join(c.args, " "), "rm -rf") {
		t.Errorf("command %+v", c)
	}
	d, _ = fake("windows", 0, "s3cret", "", nil)
	if v, err := d.Secret(context.Background(), q); v != "s3cret" || err != nil {
		t.Errorf("secret %q %v", v, err)
	}
	d, _ = fake("windows", 1, "", "Add-Type: cannot load", nil)
	if _, err := d.Secret(context.Background(), q); !errors.Is(err, app.ErrNoPrompt) {
		t.Errorf("failure: %v", err)
	}
}

func TestLinux(t *testing.T) {
	d, _ := fake("linux", 0, "", "", nil, "zenity")
	if _, err := d.Confirm(context.Background(), q); !errors.Is(err, app.ErrNoPrompt) {
		t.Errorf("no session: %v", err)
	}
	desktop := map[string]string{"WAYLAND_DISPLAY": "wayland-0"}
	d, _ = fake("linux", 0, "", "", desktop)
	if _, err := d.Confirm(context.Background(), q); !errors.Is(err, app.ErrNoPrompt) {
		t.Errorf("no dialog program: %v", err)
	}
	d, ran := fake("linux", 1, "", "", desktop, "kdialog")
	if ok, err := d.Confirm(context.Background(), q); ok || err != nil || (*ran)[0].name != "kdialog" {
		t.Errorf("kdialog no: %v %v %+v", ok, err, *ran)
	}
	d, ran = fake("linux", 0, "pw\n", "", desktop, "zenity", "kdialog")
	if v, err := d.Secret(context.Background(), q); v != "pw" || err != nil || (*ran)[0].name != "zenity" || !slices.Contains((*ran)[0].args, "--hide-text") {
		t.Errorf("zenity secret %q %v %+v", v, err, *ran)
	}
}
