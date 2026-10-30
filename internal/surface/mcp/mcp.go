// Package mcp is UGT's MCP server over stdio: the use cases the CLI offers,
// as tools for agents. Its standing instructions route "Use the GitHub Thing"
// and GitHub links with a job to them, so an AI app connected to UGT uses it
// instead of installing projects itself.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/auscaster/ugt/internal/app"
	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/job"
)

// Instructions is what an AI app is told when it connects.
const Instructions = `UGT (Use the GitHub Thing) gets GitHub projects working on this machine and does the user's task with them, in containers it manages.

When the user says "Use the GitHub Thing" or "ugt", or gives a GitHub repository and something they want from it, use these tools. Do not clone, install or run the project yourself.

1. ugt_find with the repository (owner/repo or its link).
2. If it has a recipe, ugt_run it. ugt_find lists what each recipe takes: give the user's files as inputs (a bare file name is looked for in their UGT folder, so they can drop files there), and anything you write yourself, such as a script or a list of URLs, as texts. Let the recipe do the work: do not fetch, convert or compute the result yourself. Results are delivered to the UGT folder; tell the user where, and what the check confirmed.
3. If it has no recipe, that is normal: make one. ugt_new starts it and returns its files. Write its services and Dockerfile, then call ugt_verify with the edited files until it passes (ugt_exec runs commands inside a service when you need to look). Then ugt_run the user's task.

If a call says the engine is not ready, call ugt_setup, and ugt_doctor when something fails for reasons outside the recipe.

Long calls (ugt_new, ugt_verify, ugt_run, ugt_exec, ugt_setup) return within about 25 seconds: with the result, or with a job to follow with ugt_status.

UGT asks the user itself, in a dialog on their screen, for a secret a recipe needs and to agree to any access a recipe declares beyond its sandbox (the internet, a folder, a device and so on). When a call is waiting on that, tell the user to look for UGT's dialog. Never ask for a secret in chat, never pass one to a tool, and never agree on the user's behalf. If a call says UGT cannot ask here, give the user the ugt allow or ugt settings set command it names, to run in a terminal. Ask the user before deleting saved data.`

// Server serves UGT's tools.
type Server struct {
	App     *app.App
	Version string
	// Wait is how long a long call waits for its job before returning it.
	Wait time.Duration

	jobs context.Context // jobs run under the server's lifetime, not a call's
}

// Run serves over stdin and stdout until the client disconnects or ctx ends.
// Jobs run under ctx, so they outlive the calls that start them.
func (s *Server) Run(ctx context.Context) error {
	return s.Serve(ctx, &sdk.StdioTransport{})
}

// Serve serves over any transport, such as an in-memory one in tests and
// tools.
func (s *Server) Serve(ctx context.Context, t sdk.Transport) error {
	return s.server(ctx).Run(ctx, t)
}

func (s *Server) server(ctx context.Context) *sdk.Server {
	if s.Wait == 0 {
		s.Wait = 25 * time.Second
	}
	s.jobs = ctx
	srv := sdk.NewServer(&sdk.Implementation{Name: "ugt", Title: "Use the GitHub Thing", Version: s.Version},
		&sdk.ServerOptions{Instructions: Instructions})
	readOnly := &sdk.ToolAnnotations{ReadOnlyHint: true}
	destructive := true

	sdk.AddTool(srv, &sdk.Tool{Name: "ugt_find", Annotations: readOnly, Description: "Find the recipes UGT has for a GitHub project, or for words describing a task. " +
		"Call it first whenever the user says \"Use the GitHub Thing\" or gives a GitHub repository with a job. No recipe is a normal answer: the result says how to make one."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in findIn) (*sdk.CallToolResult, app.FindResult, error) {
			res, err := s.App.Find(ctx, in.Query)
			return nil, res, err
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "ugt_run", Description: "Do the user's task with a GitHub project's recipe, on their files: it builds what it needs, runs the task and its check, " +
		"and delivers the results to the user's UGT folder. A service recipe keeps running and returns its local addresses. " +
		"A recipe that needs a secret, or access beyond its sandbox, first asks the user in UGT's own dialog. Returns the result, or a job for ugt_status."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in runIn) (*sdk.CallToolResult, jobAnswer, error) {
			found, err := s.App.Find(ctx, in.Project)
			if err != nil {
				return nil, jobAnswer{}, err
			}
			if found.Project != "" && len(found.Recipes) == 0 {
				return nil, jobAnswer{State: "no-recipe", Next: strings.Join(found.Next, " ")}, nil
			}
			opts := app.RunOptions{Project: in.Project, Task: in.Task, Inputs: in.Inputs, Texts: in.Texts, To: in.To, Values: in.Settings}
			return s.long(ctx, "run", in.Project, func(ctx context.Context, progress func(string, string)) (any, error) {
				opts.Progress = progress
				res, err := s.App.Run(ctx, opts)
				var none *catalogue.NoRecipe
				if errors.As(err, &none) {
					return nil, fmt.Errorf("%w; make one with ugt_new", err)
				}
				return res, err
			})
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "ugt_status", Annotations: readOnly, Description: "Report a job started by ugt_new, ugt_verify, ugt_run or ugt_exec: its state, progress and, once finished, its result. " +
		"Waits up to wait_seconds (default 20) for it to finish."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in statusIn) (*sdk.CallToolResult, jobAnswer, error) {
			wait := 20 * time.Second
			if in.WaitSeconds > 0 {
				wait = min(time.Duration(in.WaitSeconds)*time.Second, 50*time.Second)
			}
			j, err := s.App.Wait(in.Job, wait)
			if err != nil {
				return nil, jobAnswer{}, err
			}
			return nil, answer(j), nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "ugt_list", Annotations: readOnly, Description: "List what UGT holds on this machine, by recipe: whether it is running, its images, saved data and results folder."},
		func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, listOut, error) {
			held, err := s.App.List(ctx)
			if held == nil {
				held = []app.Installed{}
			}
			return nil, listOut{Recipes: held}, err
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "ugt_stop", Description: "Stop a recipe's running services, keeping everything else."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in recipeIn) (*sdk.CallToolResult, app.Change, error) {
			ch, err := s.App.Stop(ctx, in.Recipe)
			return nil, ch, err
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "ugt_remove", Annotations: &sdk.ToolAnnotations{DestructiveHint: &destructive},
		Description: "Remove what UGT created for a recipe: its containers, images and workspaces. Its saved data and the results in the UGT folder stay."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in recipeIn) (*sdk.CallToolResult, app.Change, error) {
			ch, err := s.App.Remove(ctx, in.Recipe)
			return nil, ch, err
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "ugt_delete_data", Annotations: &sdk.ToolAnnotations{DestructiveHint: &destructive},
		Description: "Delete a recipe's saved data (its named volumes) for good. Only after the user has agreed in so many words; set user_agreed to true to say they did."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in deleteIn) (*sdk.CallToolResult, app.Change, error) {
			if !in.UserAgreed {
				return nil, app.Change{}, errors.New("deleting saved data is for good: ask the user, and call again with user_agreed true once they agree")
			}
			ch, err := s.App.DeleteData(ctx, in.Recipe)
			return nil, ch, err
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "ugt_new", Description: "Start a recipe for a GitHub project and task when ugt_find has none. It adopts the project's own Compose file when there is one, " +
		"otherwise writes a blank recipe whose Dockerfile builds the pinned commit. Returns the recipe's folder and files, what to write next, and anything to resolve."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in newIn) (*sdk.CallToolResult, jobAnswer, error) {
			opts := app.NewOptions{Project: in.Project, Task: in.Task, What: in.What, Kind: in.Kind, From: in.From, Blank: in.Blank}
			return s.long(ctx, "new", in.Project+"/"+in.Task, func(ctx context.Context, progress func(string, string)) (any, error) {
				res, err := s.App.New(ctx, opts)
				if err != nil {
					return nil, err
				}
				files, err := s.App.ReadRecipe(res.Dir)
				return newOut{NewResult: res, Contents: files}, err
			})
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "ugt_verify", Description: "Verify a recipe folder the way every machine runs it: build, then the sample, task, check and sample-check services, " +
		"then the check-strength test, which fails any check that accepts broken outputs. Pass edited files to write them into the folder first, when it is one ugt_new made. " +
		"A failed report names the step, its log and a diagnosis. from resumes the last verification at a step, with its image, to iterate quickly."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in verifyIn) (*sdk.CallToolResult, jobAnswer, error) {
			if len(in.Files) > 0 {
				if _, err := s.App.WriteRecipe(in.Dir, in.Files); err != nil {
					return nil, jobAnswer{}, err
				}
			}
			opts := app.VerifyOptions{From: in.From}
			return s.long(ctx, "verify", in.Dir, func(ctx context.Context, progress func(string, string)) (any, error) {
				opts.Progress = progress
				return s.App.Verify(ctx, in.Dir, opts)
			})
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "ugt_exec", Description: "Run a command inside one of a recipe's services, in the workspace and with the image of its last verification, " +
		"to look at the real environment: the outputs it made, what the image holds, what a check sees. Returns the exit code and the output's last 20 KB."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in execIn) (*sdk.CallToolResult, jobAnswer, error) {
			return s.long(ctx, "exec", in.Dir, func(ctx context.Context, progress func(string, string)) (any, error) {
				var out tailBuffer
				res, err := s.App.Exec(ctx, in.Dir, in.Service, in.Command, &out)
				return execOut{ExecResult: res, Output: out.String()}, err
			})
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "ugt_submit", Description: "Share a recipe with the catalogue when the user asks to: it verifies the recipe (or uses a passing verification of the same revision), " +
		"refuses anything personal in it (the user's paths, secrets, input files, keys) with the file and line, and opens a pull request with the user's GitHub login (ugt_login signs them in). " +
		"Try dry_run first to show the user what would be sent."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in submitIn) (*sdk.CallToolResult, jobAnswer, error) {
			return s.long(ctx, "submit", in.Dir, func(ctx context.Context, progress func(string, string)) (any, error) {
				res, err := s.App.Submit(ctx, in.Dir, app.SubmitOptions{Agent: in.Agent, DryRun: in.DryRun, Progress: progress})
				if errors.Is(err, app.ErrNotShareable) {
					return res, fmt.Errorf("%w; the result lists what to fix", err)
				}
				return res, err
			})
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "ugt_login", Description: "Sign the user in to GitHub, which sharing a recipe needs. Its progress gives a code for the user to enter at the address it names, in their own browser; " +
		"the job finishes when they approve it. The login goes to the system's secret store; you never see it."},
		func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, jobAnswer, error) {
			return s.long(ctx, "login", "", func(ctx context.Context, progress func(string, string)) (any, error) {
				return s.App.Login(ctx, func(l app.DeviceLogin) {
					progress("login", fmt.Sprintf("Ask the user to open %s and enter the code %s; this waits until they approve it.", l.URL, l.Code))
				})
			})
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "ugt_doctor", Annotations: readOnly, Description: "Check this machine for UGT: the runtime, the engine and its memory, and the network it needs, each with the exact fix. " +
		"Call it when a run or verification fails for reasons outside the recipe."},
		func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, app.DoctorReport, error) {
			rep, err := s.App.Doctor(ctx)
			return nil, rep, err
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "ugt_setup", Description: "Set this machine up to run recipes: on Windows, UGT's own engine in WSL; elsewhere, the engine already there. " +
		"Installing WSL needs admin approval and may restart Windows: set allow_admin only once the user has agreed to that."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in setupIn) (*sdk.CallToolResult, jobAnswer, error) {
			return s.long(ctx, "setup", "", func(ctx context.Context, progress func(string, string)) (any, error) {
				return s.App.Setup(ctx, app.SetupOptions{AllowAdmin: in.AllowAdmin, Progress: progress})
			})
		})
	return srv
}

type submitIn struct {
	Dir    string `json:"dir" jsonschema:"the recipe folder"`
	Agent  string `json:"agent,omitempty" jsonschema:"who made and verified it, for the pull request, such as your name and model"`
	DryRun bool   `json:"dry_run,omitempty" jsonschema:"show what would be proposed, and change nothing"`
}

type setupIn struct {
	AllowAdmin bool `json:"allow_admin,omitempty" jsonschema:"true only when the user agreed to admin approval and a possible restart"`
}

// long starts work as a job and waits for it a while, so a quick call
// returns its result and a slow one its job.
func (s *Server) long(_ context.Context, kind, subject string, work app.Work) (*sdk.CallToolResult, jobAnswer, error) {
	j := s.App.Start(s.jobs, kind, subject, work)
	j, err := s.App.Wait(j.ID, s.Wait)
	if err != nil {
		return nil, jobAnswer{}, err
	}
	return nil, answer(j), nil
}

func answer(j job.Job) jobAnswer {
	a := jobAnswer{Job: j.ID, State: string(j.State), Result: j.Result, Error: j.Error}
	if n := len(j.Events); n > 0 {
		a.Progress = j.Events[max(0, n-8):]
	}
	if !j.State.Done() {
		a.Next = fmt.Sprintf("Still running. Call ugt_status with job %s to follow it.", j.ID)
	}
	return a
}

type findIn struct {
	Query string `json:"query" jsonschema:"a GitHub project as owner/repo or its link, or words describing the task"`
}

type runIn struct {
	Project  string            `json:"project" jsonschema:"the GitHub project, as owner/repo or its link"`
	Task     string            `json:"task,omitempty" jsonschema:"the recipe's task, when the project has several"`
	Inputs   []string          `json:"inputs,omitempty" jsonschema:"the user's files, as paths or as NAME=PATH; a bare file name is looked for in the UGT folder"`
	Texts    map[string]string `json:"texts,omitempty" jsonschema:"inputs you write yourself, by input name, such as a script or a list of URLs; ugt_find lists each recipe's inputs"`
	To       string            `json:"to,omitempty" jsonschema:"a folder for the results, when the user names one"`
	Settings map[string]string `json:"settings,omitempty" jsonschema:"value settings by name; never secrets"`
}

type statusIn struct {
	Job         string `json:"job" jsonschema:"the job ID"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"how long to wait for it to finish, up to 50"`
}

type recipeIn struct {
	Recipe string `json:"recipe" jsonschema:"the recipe, as owner/repo/task, or owner/repo when UGT holds one of its recipes"`
}

type deleteIn struct {
	Recipe     string `json:"recipe" jsonschema:"the recipe, as owner/repo/task"`
	UserAgreed bool   `json:"user_agreed" jsonschema:"true only when the user has agreed to deleting the saved data for good"`
}

type newIn struct {
	Project string `json:"project" jsonschema:"the GitHub project, as owner/repo or its link"`
	Task    string `json:"task" jsonschema:"a slug naming what the recipe produces, such as subtitles or markdown"`
	What    string `json:"what,omitempty" jsonschema:"the task in plain words, as the user would say it"`
	Kind    string `json:"kind,omitempty" jsonschema:"job (runs to completion and writes files) or service (keeps running); inferred when left out"`
	From    string `json:"from,omitempty" jsonschema:"a Compose file to adopt: a path in the project, or a github.com link to a file in any repository"`
	Blank   bool   `json:"blank,omitempty" jsonschema:"start blank even when the project ships a Compose file"`
}

type newOut struct {
	app.NewResult
	Contents map[string]string `json:"contents"`
}

type verifyIn struct {
	Dir   string            `json:"dir" jsonschema:"the recipe folder, as ugt_new returned it"`
	Files map[string]string `json:"files,omitempty" jsonschema:"files to write into the folder first, by path relative to it, with their full new contents"`
	From  string            `json:"from,omitempty" jsonschema:"resume the last verification at sample, task, check or sample-check, with its image"`
}

type execIn struct {
	Dir     string   `json:"dir" jsonschema:"the recipe folder"`
	Service string   `json:"service" jsonschema:"the service to run in, such as task or check"`
	Command []string `json:"command" jsonschema:"the program and its arguments, such as [\"sh\", \"-c\", \"ls -la outputs\"]"`
}

type execOut struct {
	app.ExecResult
	Output string `json:"output"`
}

type listOut struct {
	Recipes []app.Installed `json:"recipes"`
}

type jobAnswer struct {
	Job      string      `json:"job,omitempty"`
	State    string      `json:"state"`
	Result   any         `json:"result,omitempty"`
	Error    string      `json:"error,omitempty"`
	Progress []job.Event `json:"progress,omitempty"`
	Next     string      `json:"next,omitempty"`
}

// tailBuffer keeps the last 20 KB written to it.
type tailBuffer struct{ data []byte }

const tailLimit = 20 << 10

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if len(b.data) > tailLimit {
		b.data = b.data[len(b.data)-tailLimit:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string { return string(b.data) }
