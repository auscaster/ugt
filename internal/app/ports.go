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
}

// Command is one program run in a runtime.
type Command struct {
	Name   string // the program, e.g. "docker"
	Args   []string
	Dir    string            // working directory, a path in the runtime
	Env    map[string]string // added to the runtime's environment
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
	// Config returns the project's resolved configuration with every profile.
	Config(ctx context.Context, p Project) (ComposeConfig, error)
	Build(ctx context.Context, p Project, log io.Writer) (exit int, err error)
	// Run runs one service to completion: compose run --rm. A command, when
	// given, replaces the service's entrypoint and command.
	Run(ctx context.Context, p Project, service string, command []string, log io.Writer) (exit int, err error)
	// Up starts the project's default services and waits for them to be healthy.
	Up(ctx context.Context, p Project, log io.Writer) (exit int, err error)
	// Logs writes the last lines each of the project's containers logged.
	Logs(ctx context.Context, p Project, log io.Writer) error
	// Stop stops running services without removing them.
	Stop(ctx context.Context, p Project, log io.Writer) error
	// Down removes the project's containers and networks, and its named
	// volumes only when volumes is true. Images always stay.
	Down(ctx context.Context, p Project, volumes bool, log io.Writer) error
	Pull(ctx context.Context, ref string, log io.Writer) error
	Tag(ctx context.Context, source, target string) error
	// ImageDigest returns the digest ("sha256:...") a published image
	// reference points at, read from its registry without pulling it.
	ImageDigest(ctx context.Context, ref string) (string, error)
}

// ComposeConfig is the part of a resolved Compose configuration UGT reads.
type ComposeConfig struct {
	Services map[string]ComposeService
}

// ComposeService is one resolved service.
type ComposeService struct {
	Image       string
	Builds      bool
	Profiles    []string
	SecurityOpt []string
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
}

// FileInfo is a regular file and its size.
type FileInfo struct {
	Path string
	Size int64
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
}

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
