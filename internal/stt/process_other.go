//go:build !windows

package stt

import "os/exec"

func configureHiddenProcess(cmd *exec.Cmd) {
	_ = cmd
}
