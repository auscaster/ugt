package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/auscaster/ugt/internal/domain/recipe"
)

// ExecResult is a command run inside a recipe's service.
type ExecResult struct {
	Exit      int    `json:"exit"`
	Service   string `json:"service"`
	Workspace string `json:"workspace"`
	Image     string `json:"image"` // the image of the verification it stepped into
}

// OutcomeError reports the exit status of a finished command.
func (r ExecResult) OutcomeError() error {
	if r.Exit != 0 {
		return fmt.Errorf("%s exited with status %d", r.Service, r.Exit)
	}
	return nil
}

// Exec runs a command inside one of a recipe's services, in the workspace of
// its last verification and with that verification's image, so an author can
// look at the real environment: the outputs it made, the files the image
// holds, what a check sees. The recipe files are refreshed first; the image is
// not rebuilt. A service recipe's stack is started first, as verification
// does, and every stack is taken down afterwards, keeping its volumes.
func (a *App) Exec(ctx context.Context, dir, service string, command []string, log io.Writer) (ExecResult, error) {
	sc, err := a.openScratch(ctx, dir, true, false)
	if err != nil {
		return ExecResult{}, err
	}
	if _, ok := sc.config.Services[service]; !ok {
		names := make([]string, 0, len(sc.config.Services))
		for name := range sc.config.Services {
			names = append(names, name)
		}
		sort.Strings(names)
		return ExecResult{}, fmt.Errorf("%s has no service %q; it has %s", sc.folder.ID, service, strings.Join(names, ", "))
	}
	res := ExecResult{Service: service, Workspace: sc.project.Dir, Image: sc.project.Env[recipe.VarImage]}
	defer func() {
		var discard bytes.Buffer
		_ = a.Engine.Down(context.WithoutCancel(ctx), sc.project, false, &discard)
	}()
	if sc.folder.Recipe.Kind == recipe.KindService {
		// a service recipe's services work against its running stack
		if exit, err := a.up(ctx, sc.project, log); exit != 0 || err != nil {
			return res, fmt.Errorf("the stack did not start (exit %d): %v; its log is above", exit, err)
		}
	}
	res.Exit, err = a.Engine.Run(ctx, sc.project, service, command, log)
	return res, err
}
