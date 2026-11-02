//go:build !windows

package main

// detached is for Windows, whose console programs get a window of their own.
func detached([]string) bool { return false }
