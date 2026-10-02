//go:build windows

package secrets

import (
	"os/exec"
	"syscall"
)

func configureDopplerCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
