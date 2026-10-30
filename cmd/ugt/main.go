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

	"github.com/auscaster/ugt/internal/adapter/docker"
	"github.com/auscaster/ugt/internal/adapter/fsstate"
	"github.com/auscaster/ugt/internal/adapter/github"
	"github.com/auscaster/ugt/internal/adapter/host"
	"github.com/auscaster/ugt/internal/adapter/localdir"
	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/workspace"
	"github.com/auscaster/ugt/internal/surface/cli"
	"github.com/auscaster/ugt/internal/surface/mcp"
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
	c := &cli.CLI{App: a, Stdout: os.Stdout, Stderr: os.Stderr, Version: version}
	c.MCP = (&mcp.Server{App: a, Version: version}).Run
	os.Exit(c.Run(ctx, os.Args[1:]))
}

// wire builds the app for this machine. Only the host runtime exists so far;
// setup will choose the WSL or VM runtime when it is UGT's own.
func wire() (*app.App, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	layout := workspace.DefaultLayout(runtime.GOOS, home, os.Getenv)
	layout.Workspaces = filepath.ToSlash(filepath.Join(layout.State, "workspaces"))
	rt := host.New(layout.Workspaces)
	return &app.App{
		Runtime: rt,
		Engine:  docker.New(rt),
		Disk:    host.Disk{},
		GitHub:  github.New(os.Getenv("GITHUB_TOKEN")),
		Catalogues: []app.Catalogue{
			localdir.New(catalogue.Local, os.DirFS(layout.Recipes())),
			localdir.New(catalogue.BuiltIn, recipes.FS),
		},
		Jobs:   fsstate.NewJobs(layout.Jobs()),
		Ledger: fsstate.NewLedger(layout.Ledger()),
		Layout: layout,
		Now:    time.Now,
	}, nil
}
