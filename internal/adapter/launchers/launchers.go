// Package launchers implements the Launchers port: a small file for each
// command a recipe provides, in the folder pipx and uv put commands in, which
// runs it through ugt. UGT marks every file it writes and never touches one
// it did not.
package launchers

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/auscaster/ugt/internal/app"
)

// mark is in every launcher UGT writes.
const mark = "Written by UGT"

// Folder is where the launchers go, and the ugt they run.
type Folder struct {
	Dir  string // ~/.local/bin; %LOCALAPPDATA%\UGT\bin on Windows
	UGT  string // the ugt program
	GOOS string
	Path string // the user's PATH
}

var _ app.Launchers = (*Folder)(nil)

// New returns the launchers' folder for goos, given the user's home folder,
// the ugt program and an environment lookup.
func New(goos, home, ugt string, getenv func(string) string) *Folder {
	dir := filepath.Join(home, ".local", "bin")
	if goos == "windows" {
		base := getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		dir = filepath.Join(base, "UGT", "bin")
	}
	return &Folder{Dir: dir, UGT: ugt, GOOS: goos, Path: getenv("PATH")}
}

func (f *Folder) file(name string) string {
	if f.GOOS == "windows" {
		return filepath.Join(f.Dir, name+".cmd")
	}
	return filepath.Join(f.Dir, name)
}

func (f *Folder) content(name string) []byte {
	note := mark + ": the command " + name + ", which a recipe provides; ugt remove takes it away."
	if f.GOOS == "windows" {
		return []byte(fmt.Sprintf("@echo off\r\nrem %s\r\n\"%s\" command %s %%*\r\n", note, f.UGT, name))
	}
	quoted := "'" + strings.ReplaceAll(f.UGT, "'", `'\''`) + "'"
	return []byte(fmt.Sprintf("#!/bin/sh\n# %s\nexec %s command %s \"$@\"\n", note, quoted, name))
}

// Install writes the launcher of a command, replacing only one UGT wrote.
func (f *Folder) Install(name string) (string, error) {
	file := f.file(name)
	if ours, err := f.ours(file); err != nil {
		return "", err
	} else if !ours {
		return "", fmt.Errorf("%s exists and UGT did not write it, so UGT leaves it alone: remove it, and run again", file)
	}
	if err := os.MkdirAll(f.Dir, 0o755); err != nil {
		return "", err
	}
	return file, os.WriteFile(file, f.content(name), 0o755)
}

// Remove removes the launcher of a command, when UGT wrote it.
func (f *Folder) Remove(name string) error {
	file := f.file(name)
	if ours, err := f.ours(file); err != nil || !ours {
		return err
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// ours reports whether file is free for UGT's launcher: not there, or one UGT
// wrote.
func (f *Folder) ours(file string) (bool, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return strings.Contains(string(data), mark), nil
}

// OnPath reports whether the launchers' folder is on the user's PATH.
func (f *Folder) OnPath() (bool, string) {
	want := filepath.Clean(f.Dir)
	for _, dir := range filepath.SplitList(f.Path) {
		dir = filepath.Clean(dir)
		if dir == want || (f.GOOS == "windows" && strings.EqualFold(dir, want)) {
			return true, f.Dir
		}
	}
	return false, f.Dir
}
