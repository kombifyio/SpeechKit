//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const wakewordSidecarCreateNoWindow = 0x08000000

// configureHiddenProcess matches the helper in internal/localllm; kept
// local to the wake-word adapter so the public API of internal/localllm
// doesn't have to grow new clients.
func configureHiddenProcess(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: wakewordSidecarCreateNoWindow,
	}
}
