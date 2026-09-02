//go:build !windows

package local

import "os/exec"

func configureHiddenProcess(cmd *exec.Cmd, lowered bool) {
	_, _ = cmd, lowered
}

// SetSubprocessPriorityLowered is a no-op outside Windows; subprocess
// priority classes are a Windows scheduling concept.
func SetSubprocessPriorityLowered(bool) {}

func subprocessPriorityLowered(override *bool) bool {
	if override != nil {
		return *override
	}
	return true
}
