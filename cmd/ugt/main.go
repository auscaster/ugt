// Command ugt is the Use the GitHub Thing helper. This file is the
// composition root: the only place adapters and surfaces meet.
package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/auscaster/ugt/internal/adapter/agents"
	catalogueservice "github.com/auscaster/ugt/internal/adapter/catalogue"
	"github.com/auscaster/ugt/internal/adapter/docker"
	"github.com/auscaster/ugt/internal/adapter/fsstate"
	"github.com/auscaster/ugt/internal/adapter/github"
	"github.com/auscaster/ugt/internal/adapter/host"
	"github.com/auscaster/ugt/internal/adapter/keychain"
	"github.com/auscaster/ugt/internal/adapter/localdir"
	"github.com/auscaster/ugt/internal/adapter/prompt"
	"github.com/auscaster/ugt/internal/adapter/wsl"
	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/workspace"
	"github.com/auscaster/ugt/internal/surface/cli"
	"github.com/auscaster/ugt/internal/surface/mcp"
	"github.com/auscaster/ugt/layer"
	"github.com/auscaster/ugt/skills"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	a, err := wire()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ugt: %v\n", err)
		os.Exit(cli.ExitBroken)
	}
	c := &cli.CLI{App: a, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Version: version}
	c.MCP = (&mcp.Server{App: a, Version: version}).Run
	os.Exit(c.Run(ctx, os.Args[1:]))
}

// wire builds the app for this machine. On Windows the engine runs in UGT's
// own WSL distro; elsewhere it is the engine already on the machine, until
// UGT's macOS VM exists. UGT_RUNTIME=host uses an existing engine on Windows
// too, such as Docker Desktop.
func wire() (*app.App, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	layout := workspace.DefaultLayout(runtime.GOOS, home, os.Getenv)
	var rt app.Runtime = host.New(filepath.Join(layout.State, "workspaces"))
	if runtime.GOOS == "windows" && os.Getenv("UGT_RUNTIME") != "host" {
		rt = wsl.New(wsl.Config{
			Distro:       layer.Distro,
			Workspaces:   layer.Workspaces,
			Engine:       layer.EngineCommand,
			Provision:    layer.Provision(),
			AlpineBranch: layer.AlpineBranch,
			InstallDir:   filepath.Join(layout.State, "wsl"),
		}, host.Files{})
	}
	layout.Workspaces = rt.Workspaces()
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	gh := github.New(sync.OnceValue(githubToken))
	return &app.App{
		Agents:  agentHosts(home),
		Self:    app.MCPServer{Command: self, Args: []string{"mcp"}},
		Runtime: rt,
		Engine:  docker.New(rt),
		Disk:    host.Disk{},
		GitHub:  gh,
		Catalogues: []app.Catalogue{
			localdir.New(catalogue.Local, os.DirFS(layout.Recipes())),
			published(gh, layout),
		},
		Secrets:  keychain.Keychain{},
		Values:   fsstate.NewValues(layout.Settings()),
		Consents: fsstate.NewConsents(layout.Consents()),
		Prompt:   prompt.New(runtime.GOOS), // the CLI asks in its terminal instead, when it has one
		Jobs:     fsstate.NewJobs(layout.Jobs()),
		Ledger:   fsstate.NewLedger(layout.Ledger()),
		Layout:   layout,
		Now:      time.Now,
	}, nil
}

// githubToken is the user's GitHub login: GITHUB_TOKEN, else the GitHub
// CLI's. It is looked up once, and only when a request needs it.
func githubToken() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	if out, err := exec.Command("gh", "auth", "token").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

// agentHosts finds the AI apps here, with the built-in skill to install in
// those that read skills.
func agentHosts(home string) *agents.Hosts {
	h := agents.New(runtime.GOOS, home)
	if skill, err := fs.Sub(skills.FS, "ugt"); err == nil {
		h.Skill = skill
	}
	return h
}

// published is the published catalogue, from the catalogue service, so
// recipes never wait for a release of ugt. UGT_CATALOGUE points it elsewhere
// for development: a catalogue service's URL, or a folder laid out like
// recipes/.
func published(gh *github.Client, layout workspace.Layout) app.Catalogue {
	switch at := os.Getenv("UGT_CATALOGUE"); {
	case strings.HasPrefix(at, "https://") || strings.HasPrefix(at, "http://"):
		return catalogueservice.New(at, gh.Token, filepath.Join(layout.Cache(), "catalogue"))
	case at != "":
		return localdir.New(catalogue.Published, os.DirFS(at))
	}
	return catalogueservice.New(catalogue.API, gh.Token, filepath.Join(layout.Cache(), "catalogue"))
}
