//go:build windows

package stt

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func configureHiddenProcess(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
