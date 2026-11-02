// Package app holds UGT's use cases, one file each, and declares in this file
// every port they need. It depends only on the domain; adapters implement the
// ports and cmd/ugt wires them in.
package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"time"

	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/compose"
	"github.com/auscaster/ugt/internal/domain/job"
	"github.com/auscaster/ugt/internal/domain/ledger"
)

// Runtime is where the engine runs: the UGT distro on Windows, UGT's VM on
// macOS, or the host's own engine. The Engine port drives the engine itself.
type Runtime interface {
	// Name is "wsl", "vm" or "host".
	Name() string
	// Status reports whether the runtime is ready, and if not, the fix.
	Status(ctx context.Context) (RuntimeStatus, error)
	// Exec runs a program in the runtime (docker, for the Engine adapter) and
	// returns its exit code; err means it could not run at all.
	Exec(ctx context.Context, c Command) (exit int, err error)
	// Files works on paths in the runtime's file system.
	Files() Files
	// Workspaces is the root of recipe workspaces, a slash-separated path in
	// the runtime's file system.
	Workspaces() string
	// Setup makes the runtime ready. Consent comes with the call, so it never
	// prompts: steps that need admin rights or a restart run only with
	// AllowAdmin, and otherwise come back as next steps.
	Setup(ctx context.Context, opts SetupOptions) (SetupResult, error)
	// Teardown removes what Setup created, with everything in it, such as the
	// WSL distro and its disk. An engine that was already there is left alone.
	Teardown(ctx context.Context) error
	// Folder makes a folder of the user's, a host path, reachable from the
	// runtime for one command, and returns its path there: itself where the
	// engine already reaches the host's files, or a mount into UGT's distro,
	// which mounts nothing else. release ends it.
	Folder(ctx context.Context, dir string) (path string, release func(), err error)
}

// SetupOptions is the consent and progress for setting a runtime up.
type SetupOptions struct {
	AllowAdmin bool
	Progress   func(step, message string)
}

// SetupResult is what setting a runtime up did, and what is left.
type SetupResult struct {
	Runtime      string   `json:"runtime"`
	Ready        bool     `json:"ready"`
	Done         []string `json:"done,omitempty"`
	NeedsRestart bool     `json:"needs_restart,omitempty"`
	Next         []string `json:"next,omitempty"`
	// Created names what setup made that UGT owns, such as its WSL distro,
	// for the ledger; empty when it uses an engine that was already there.
	Created string `json:"created,omitempty"`
}

// Command is one program run in a runtime.
type Command struct {
	Name   string // the program, e.g. "docker"
	Args   []string
	Dir    string            // working directory, a path in the runtime
	Env    map[string]string // added to the runtime's environment
	Stdin  io.Reader         // nil for none
	Stdout io.Writer
	Stderr io.Writer
}

// RuntimeStatus is whether a runtime is ready.
type RuntimeStatus struct {
	Ready   bool   `json:"ready"`
	Problem string `json:"problem,omitempty"` // what is wrong, when not ready
	Fix     string `json:"fix,omitempty"`     // the exact fix
}

// EngineInfo is what placement and verification need to know about the engine.
type EngineInfo struct {
	Version     string `json:"version"` // e.g. "29.4.0"
	Arch        string `json:"arch"`    // "amd64" or "arm64"
	MemoryBytes int64  `json:"memory_bytes"`
	CPUs        int    `json:"cpus"`
}

// Project is one Compose project in the runtime.
type Project struct {
	Name  string            // the Compose project name
	Dir   string            // the project directory: the workspace, a runtime path
	Files []string          // Compose files, relative to Dir, in merge order
	Env   map[string]string // UGT_IMAGE, UGT_ARCH and settings
}

// Engine drives Docker Engine with Compose and Buildx. Exit codes are the
// services' own results; errors mean the engine could not be driven at all.
type Engine interface {
	Info(ctx context.Context) (EngineInfo, error)
	// Config returns the project's resolved Compose model with every profile.
	Config(ctx context.Context, p Project) (compose.Model, error)
	Build(ctx context.Context, p Project, log io.Writer) (exit int, err error)
	// Run runs one service to completion: compose run --rm. A command, when
	// given, replaces the service's entrypoint and command.
	Run(ctx context.Context, p Project, service string, command []string, log io.Writer) (exit int, err error)
	// Attach runs one service for the user, connected to them: compose run
	// --rm with args after its entrypoint, as a command or an MCP server.
	Attach(ctx context.Context, p Project, service string, args []string, to Attached) (exit int, err error)
	// Up starts the project's default services and waits for them to be healthy.
	Up(ctx context.Context, p Project, log io.Writer) (exit int, err error)
	// Logs writes the last lines each of the project's containers logged.
	Logs(ctx context.Context, p Project, log io.Writer) error
	// Stop stops running services without removing them.
	Stop(ctx context.Context, p Project, log io.Writer) error
	// Down removes the project's containers and networks, and its named
	// volumes only when volumes is true. Images always stay.
	Down(ctx context.Context, p Project, volumes bool, log io.Writer) error
	// Running reports whether any of the project's containers are running.
	Running(ctx context.Context, p Project) (bool, error)
	Pull(ctx context.Context, ref string, log io.Writer) error
	Tag(ctx context.Context, source, target string) error
	// ImageExists reports whether the engine holds an image.
	ImageExists(ctx context.Context, ref string) (bool, error)
	// RemoveImage removes an image the engine holds; one already gone is no error.
	RemoveImage(ctx context.Context, ref string) error
	// Volumes lists the named volumes a Compose project created, by its name,
	// whether or not its files still exist.
	Volumes(ctx context.Context, project string) ([]string, error)
	// RemoveVolume removes a named volume; one already gone is no error.
	RemoveVolume(ctx context.Context, name string) error
	// ImageDigest returns the digest ("sha256:...") a published image
	// reference points at, read from its registry without pulling it.
	ImageDigest(ctx context.Context, ref string) (string, error)
}

// Attached is how a service run for the user reaches them.
type Attached struct {
	// Folder is mounted at its own path as the working directory: the folder
	// a command is run in. "" mounts nothing.
	Folder string
	// Terminal gives the program the user's terminal, for a program that
	// asks or draws; false keeps the streams plain, as MCP needs.
	Terminal bool
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
}

// Files works on paths in a runtime's file system.
type Files interface {
	MkdirAll(path string) error
	RemoveAll(path string) error
	// CopyIn copies a host file or folder into the runtime.
	CopyIn(hostPath, path string) error
	// CopyOut copies a runtime file or folder to the host.
	CopyOut(path, hostPath string) error
	// Copy copies a file or folder within the runtime.
	Copy(source, target string) error
	// List returns every regular file under dir, recursively.
	List(dir string) ([]FileInfo, error)
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte) error
	// Share lets every container user write to the folder at path, whatever
	// user an image runs as: workspaces' inputs/ and outputs/ are written by
	// the recipe's services.
	Share(path string) error
}

// Disk is the user's own file system, where recipe folders are written and
// read and input files come from. Paths are host paths; the runtime's file
// system is Files.
type Disk interface {
	// FS reads the folder dir.
	FS(dir string) fs.FS
	// WriteFile writes a file, creating the folders it needs.
	WriteFile(path string, data []byte) error
	// Exists reports whether anything is at path.
	Exists(path string) (bool, error)
	// RemoveAll removes path and everything in it.
	RemoveAll(path string) error
}

// FileInfo is a regular file and its size.
type FileInfo struct {
	Path string
	Size int64
}

// Catalogue is one source of recipes. It is asked about one project, or for
// recipe IDs that match some words, never for everything it holds, so a
// catalogue of thousands of recipes costs the same as one of four.
type Catalogue interface {
	Source() catalogue.Source
	// Project lists the recipes the source holds for one GitHub project.
	Project(ctx context.Context, owner, repo string) ([]catalogue.Entry, error)
	// Search lists the recipes whose ID holds every word.
	Search(ctx context.Context, words []string) ([]catalogue.Entry, error)
	// Open returns a recipe's folder, by its ID.
	Open(ctx context.Context, id string) (fs.FS, error)
}

// RunReports tells the catalogue service how a run of one of its recipes went.
type RunReports interface {
	Report(ctx context.Context, r catalogue.Run) error
}

// GitHub reads public repositories.
type GitHub interface {
	Repo(ctx context.Context, owner, name string) (Repo, error)
	// Commit resolves a branch, tag or commit to its full SHA.
	Commit(ctx context.Context, owner, name, ref string) (string, error)
	// List returns the entries of a folder at a commit; "" is the root.
	List(ctx context.Context, owner, name, commit, dir string) ([]Entry, error)
	// File returns a file's bytes at a commit.
	File(ctx context.Context, owner, name, commit, path string) ([]byte, error)
	// Propose opens a pull request with a folder's files, as the user, from a
	// fork when they cannot push to the repository, and returns its address.
	// Proposing again from the same branch updates the pull request.
	Propose(ctx context.Context, p Proposal) (string, error)
	// StartLogin begins GitHub's device flow: a code the user enters at an
	// address, in their own browser.
	StartLogin(ctx context.Context) (DeviceLogin, error)
	// FinishLogin waits until the user approves the login, and returns the
	// token and the login it belongs to.
	FinishLogin(ctx context.Context, l DeviceLogin) (token, user string, err error)
}

// DeviceLogin is a device-flow login waiting for the user's approval.
type DeviceLogin struct {
	Code     string        `json:"code"` // what the user enters
	URL      string        `json:"url"`  // where they enter it
	Expires  time.Time     `json:"expires"`
	Device   string        `json:"-"` // GitHub's handle on it, never shown
	Interval time.Duration `json:"-"`
}

// Proposal is a pull request that sets one folder's files.
type Proposal struct {
	Repo    string            // owner/name of the repository
	Dir     string            // the folder, as recipes/owner/repo/task
	Files   map[string][]byte // by path inside Dir; whatever else is in Dir is removed
	Branch  string
	Message string // the commit's
	Title   string
	Body    string
}

// ErrNoLogin is wrapped when an action needs the user's GitHub login.
var ErrNoLogin = errors.New("this needs your GitHub login: sign in with ugt login (ugt_login in an AI app)")

// Repo is what UGT reads about a GitHub repository.
type Repo struct {
	Owner, Name   string // as GitHub spells them
	DefaultBranch string
	Licence       string // SPDX identifier; "" when GitHub detects none
	Language      string // the main language, as GitHub names it
	Description   string
}

// Entry is one file or folder in a repository.
type Entry struct {
	Path string // slash path from the repository root
	Dir  bool
}

// Secrets stores secret settings in the system's own secret store, by recipe
// and name. No surface ever receives a value from it.
type Secrets interface {
	Get(ctx context.Context, recipe, name string) (value string, ok bool, err error)
	Set(ctx context.Context, recipe, name, value string) error
	Delete(ctx context.Context, recipe, name string) error
}

// Values stores value settings, which are not secret, by recipe and name.
type Values interface {
	Get(recipe string) (map[string]string, error)
	Set(recipe, name, value string) error
	Delete(recipe, name string) error
}

// Consents remembers the access the user agreed to, by recipe: each agreement
// is the digest of exactly what was granted, so a change asks again.
type Consents interface {
	Agreed(recipe, digest string) (bool, error)
	Agree(recipe, digest string) error
}

// Prompt asks the user directly, never through an agent: in the terminal when
// the command line has one, otherwise in a dialog of the desktop.
type Prompt interface {
	// Confirm asks the user to agree, and reports whether they did.
	Confirm(ctx context.Context, q Question) (bool, error)
	// Secret asks the user to type a value that is never shown.
	Secret(ctx context.Context, q Question) (string, error)
}

// Question is what a prompt shows.
type Question struct {
	Title   string
	Message string
	Yes, No string // a confirmation's answers; "Allow" and "Don't allow" when empty
}

// ErrNoPrompt is what a Prompt returns when it cannot reach the user here: no
// terminal and no desktop.
var ErrNoPrompt = errors.New("UGT cannot ask the user here: there is no terminal or desktop")

// AgentHosts finds the AI apps on this machine and connects UGT's MCP server
// to each, through that app's own documented mechanism.
type AgentHosts interface {
	// Find lists the AI apps installed here, and whether UGT is connected to each.
	Find(ctx context.Context) ([]AgentHost, error)
	// Connect adds the server to an app and says what changed.
	Connect(ctx context.Context, name string, server MCPServer) (string, error)
	// Disconnect removes it and says what changed.
	Disconnect(ctx context.Context, name string) (string, error)
}

// AgentHost is one AI app.
type AgentHost struct {
	Name      string `json:"name"` // claude-desktop, claude-code, codex, cursor, vscode
	Title     string `json:"title"`
	Connected bool   `json:"connected"`
	Where     string `json:"where"` // its configuration file, or the command that configures it
}

// MCPServer is how an AI app starts UGT's MCP server.
type MCPServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// Launchers put the commands recipes provide on the user's PATH: a small file
// for each, which runs it through ugt.
type Launchers interface {
	// Install writes the launcher of the command name, which runs
	// `ugt command name`, and returns where. It never replaces a file it did
	// not write.
	Install(name string) (string, error)
	// Remove removes the launcher of the command name, if it wrote it.
	Remove(name string) error
	// OnPath reports whether the launchers' folder is on the user's PATH,
	// and the folder.
	OnPath() (bool, string)
}

// FreePort returns a free port on 127.0.0.1: want, when it is free, so an
// address stays the same from one run to the next; otherwise any.
type FreePort func(want int) (int, error)

// ErrNotFound is wrapped by adapters when what was asked for does not exist.
var ErrNotFound = errors.New("not found")

// Jobs persists jobs so their status survives the process that ran them.
type Jobs interface {
	Save(j job.Job) error
	Load(id string) (job.Job, error)
	List() ([]job.Job, error)
}

// Ledger records what UGT creates.
type Ledger interface {
	Record(e ledger.Entry) error
	Entries() ([]ledger.Entry, error)
}

// Clock tells the time; tests fix it.
type Clock func() time.Time
