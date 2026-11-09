// Package prompt implements the Prompt port with the desktop's own dialogs:
// AppleScript's on macOS, a Windows Forms dialog through PowerShell on
// Windows, and zenity or kdialog on a Linux desktop. The user answers UGT
// directly, so an agent never answers for them and never sees a secret. The
// question's text reaches each program as an argument or an environment
// variable, never inside a script.
package prompt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/usegeekthing/ugt/internal/app"
)

// Desktop asks through the desktop of the operating system it was made for.
type Desktop struct {
	goos string
	env  func(string) string
	look func(string) (string, error)
	run  func(ctx context.Context, c command) (stdout, stderr string, exit int, err error)
}

// command is one program to run, with what it adds to the environment.
type command struct {
	name string
	args []string
	env  []string
}

var _ app.Prompt = (*Desktop)(nil)

// New returns the desktop prompt for goos.
func New(goos string) *Desktop {
	return &Desktop{goos: goos, env: os.Getenv, look: exec.LookPath, run: run}
}

// declined is the exit code each dialog here gives when the user says no.
const declined = 3

func (d *Desktop) Confirm(ctx context.Context, q app.Question) (bool, error) {
	yes, no := q.Yes, q.No
	if yes == "" {
		yes, no = "Allow", "Don't allow"
	}
	c, err := d.confirm(q, yes, no)
	if err != nil {
		return false, err
	}
	_, stderr, exit, err := d.run(ctx, c)
	switch {
	case err != nil:
		return false, fmt.Errorf("%w (%v)", app.ErrNoPrompt, err)
	case exit == 0:
		return true, nil
	case exit == declined || d.cancelled(exit, stderr):
		return false, nil
	}
	return false, fmt.Errorf("%w (%s exited %d: %s)", app.ErrNoPrompt, c.name, exit, strings.TrimSpace(stderr))
}

func (d *Desktop) Secret(ctx context.Context, q app.Question) (string, error) {
	c, err := d.secret(q)
	if err != nil {
		return "", err
	}
	stdout, stderr, exit, err := d.run(ctx, c)
	switch {
	case err != nil:
		return "", fmt.Errorf("%w (%v)", app.ErrNoPrompt, err)
	case exit == 0:
		return strings.TrimRight(stdout, "\r\n"), nil
	case exit == declined || d.cancelled(exit, stderr):
		return "", nil
	}
	return "", fmt.Errorf("%w (%s exited %d: %s)", app.ErrNoPrompt, c.name, exit, strings.TrimSpace(stderr))
}

// cancelled reports whether a dialog's own cancel gave this exit: the user
// declined rather than the dialog failing.
func (d *Desktop) cancelled(exit int, stderr string) bool {
	switch d.goos {
	case "darwin":
		return exit == 1 && strings.Contains(stderr, "(-128)") // "User canceled."
	case "linux":
		return exit == 1
	}
	return false
}

func (d *Desktop) confirm(q app.Question, yes, no string) (command, error) {
	switch d.goos {
	case "darwin":
		return command{name: "osascript", args: []string{
			"-e", "on run argv",
			"-e", "display dialog (item 2 of argv) with title (item 1 of argv) buttons {(item 4 of argv), (item 3 of argv)} default button (item 4 of argv) cancel button (item 4 of argv) with icon caution",
			"-e", "end run",
			q.Title, q.Message, yes, no,
		}}, nil
	case "windows":
		return powershell(confirmScript, q, yes, no), nil
	case "linux":
		switch tool, err := d.linuxTool(); tool {
		case "zenity":
			return command{name: tool, args: []string{"--question", "--no-markup", "--default-cancel", "--title", q.Title, "--text", q.Message, "--ok-label", yes, "--cancel-label", no}}, nil
		case "kdialog":
			return command{name: tool, args: []string{"--title", q.Title, "--yes-label", yes, "--no-label", no, "--yesno", q.Message}}, nil
		default:
			return command{}, err
		}
	}
	return command{}, app.ErrNoPrompt
}

func (d *Desktop) secret(q app.Question) (command, error) {
	switch d.goos {
	case "darwin":
		return command{name: "osascript", args: []string{
			"-e", "on run argv",
			"-e", `return text returned of (display dialog (item 2 of argv) with title (item 1 of argv) default answer "" with hidden answer buttons {"Cancel", "Save"} default button "Save" cancel button "Cancel" with icon note)`,
			"-e", "end run",
			q.Title, q.Message,
		}}, nil
	case "windows":
		return powershell(secretScript, q, "Save", "Cancel"), nil
	case "linux":
		switch tool, err := d.linuxTool(); tool {
		case "zenity":
			return command{name: tool, args: []string{"--entry", "--hide-text", "--no-markup", "--title", q.Title, "--text", q.Message}}, nil
		case "kdialog":
			return command{name: tool, args: []string{"--title", q.Title, "--password", q.Message}}, nil
		default:
			return command{}, err
		}
	}
	return command{}, app.ErrNoPrompt
}

// linuxTool is the dialog program of a Linux desktop session, when there is
// one.
func (d *Desktop) linuxTool() (string, error) {
	if d.env("DISPLAY") == "" && d.env("WAYLAND_DISPLAY") == "" {
		return "", fmt.Errorf("%w (no desktop session)", app.ErrNoPrompt)
	}
	for _, tool := range []string{"zenity", "kdialog"} {
		if _, err := d.look(tool); err == nil {
			return tool, nil
		}
	}
	return "", fmt.Errorf("%w (neither zenity nor kdialog is installed)", app.ErrNoPrompt)
}

// powershell runs a Windows Forms script, handing it the question through
// the environment.
func powershell(script string, q app.Question, yes, no string) command {
	return command{
		name: "powershell.exe",
		args: []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script},
		env:  []string{"UGT_PROMPT_TITLE=" + q.Title, "UGT_PROMPT_MESSAGE=" + q.Message, "UGT_PROMPT_YES=" + yes, "UGT_PROMPT_NO=" + no},
	}
}

// environ is this process's environment, with what c adds. Windows
// PowerShell gets no PSModulePath: one inherited from PowerShell 7, where
// ugt is often started, points it at modules it cannot load, and then even
// Add-Type is missing. Without one, it finds its own.
func environ(c command) []string {
	env := os.Environ()
	if c.name == "powershell.exe" {
		env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(strings.ToUpper(kv), "PSMODULEPATH=") })
	}
	return append(env, c.env...)
}

// The Windows dialogs: a topmost form, so the question is never hidden behind
// the AI app that asked for it. Each exits 0 for yes and 3 for no. As on every
// desktop, a confirmation's default answer is no.
const formsPrelude = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.Application]::EnableVisualStyles()
$f = New-Object System.Windows.Forms.Form -Property @{ Text = $env:UGT_PROMPT_TITLE; TopMost = $true; StartPosition = 'CenterScreen'; FormBorderStyle = 'FixedDialog'; MaximizeBox = $false; MinimizeBox = $false; AutoSize = $true; AutoSizeMode = 'GrowAndShrink'; Padding = 12 }
$p = New-Object System.Windows.Forms.FlowLayoutPanel -Property @{ FlowDirection = 'TopDown'; AutoSize = $true; WrapContents = $false }
$l = New-Object System.Windows.Forms.Label -Property @{ Text = $env:UGT_PROMPT_MESSAGE; AutoSize = $true; MaximumSize = New-Object System.Drawing.Size(520, 0) }
$p.Controls.Add($l)
`

const formsButtons = `$b = New-Object System.Windows.Forms.FlowLayoutPanel -Property @{ FlowDirection = 'RightToLeft'; AutoSize = $true; Dock = 'Fill' }
$yes = New-Object System.Windows.Forms.Button -Property @{ Text = $env:UGT_PROMPT_YES; DialogResult = 'OK'; AutoSize = $true }
$no = New-Object System.Windows.Forms.Button -Property @{ Text = $env:UGT_PROMPT_NO; DialogResult = 'Cancel'; AutoSize = $true }
$b.Controls.AddRange(@($no, $yes)); $p.Controls.Add($b); $f.Controls.Add($p)
$f.CancelButton = $no
`

const confirmScript = formsPrelude + formsButtons + `$f.AcceptButton = $no
if ($f.ShowDialog() -eq 'OK') { exit 0 } else { exit 3 }
`

const secretScript = formsPrelude + `$t = New-Object System.Windows.Forms.TextBox -Property @{ UseSystemPasswordChar = $true; Width = 520 }
$p.Controls.Add($t)
` + formsButtons + `$f.AcceptButton = $yes
if ($f.ShowDialog() -eq 'OK') { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; [Console]::Out.Write($t.Text); exit 0 } else { exit 3 }
`

func run(ctx context.Context, c command) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, c.name, c.args...)
	cmd.Env = environ(c)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return stdout.String(), stderr.String(), exit.ExitCode(), nil
	}
	return stdout.String(), stderr.String(), 0, err
}
