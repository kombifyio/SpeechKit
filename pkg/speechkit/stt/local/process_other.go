//go:build !windows

package local

import "os/exec"

func configureHiddenProcess(cmd *exec.Cmd) {
	_ = cmd
}

// SetSubprocessPriorityLowered is a no-op outside Windows; subprocess
// priority classes are a Windows scheduling concept.
func SetSubprocessPriorityLowered(bool) {}
