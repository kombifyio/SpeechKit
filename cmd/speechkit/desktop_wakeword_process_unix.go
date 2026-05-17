//go:build !windows

package main

import "os/exec"

// configureHiddenProcess is a no-op on non-Windows platforms — POSIX shells
// do not pop console windows for child processes the way Windows does.
func configureHiddenProcess(_ *exec.Cmd) {}
