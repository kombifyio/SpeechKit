//go:build windows

package codex

import (
	"os/exec"
	"syscall"
)

// configureSysProcAttr hides the child console window so spawning codex from
// the desktop app never flashes a terminal.
func configureSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
