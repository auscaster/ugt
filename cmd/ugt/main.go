// Command ugt is the Use the GitHub Thing helper. This file is the
// composition root: the only place adapters and surfaces meet.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"github.com/auscaster/ugt/internal/adapter/agents"
	"github.com/auscaster/ugt/internal/adapter/docker"
	"github.com/auscaster/ugt/internal/adapter/fsstate"
	"github.com/auscaster/ugt/internal/adapter/github"
	"github.com/auscaster/ugt/internal/adapter/host"
	"github.com/auscaster/ugt/internal/adapter/keychain"
	"github.com/auscaster/ugt/internal/adapter/localdir"
	"github.com/auscaster/ugt/internal/adapter/wsl"
	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/workspace"
	"github.com/auscaster/ugt/internal/surface/cli"
	"github.com/auscaster/ugt/internal/surface/mcp"
	"github.com/auscaster/ugt/layer"
	"github.com/auscaster/ugt/recipes"
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
	return &app.App{
		Agents:  agents.New(runtime.GOOS, home),
		Self:    app.MCPServer{Command: self, Args: []string{"mcp"}},
		Runtime: rt,
		Engine:  docker.New(rt),
		Disk:    host.Disk{},
		GitHub:  github.New(os.Getenv("GITHUB_TOKEN")),
		Catalogues: []app.Catalogue{
			localdir.New(catalogue.Local, os.DirFS(layout.Recipes())),
			localdir.New(catalogue.BuiltIn, recipes.FS),
		},
		Secrets: keychain.Keychain{},
		Values:  fsstate.NewValues(layout.Settings()),
		Jobs:    fsstate.NewJobs(layout.Jobs()),
		Ledger:  fsstate.NewLedger(layout.Ledger()),
		Layout:  layout,
		Now:     time.Now,
	}, nil
}
