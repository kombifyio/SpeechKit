//go:build !windows

package stt

import "os/exec"

func configureHiddenProcess(cmd *exec.Cmd) {
	_ = cmd
}

// SetSubprocessPriorityLowered is a no-op outside Windows; subprocess
// priority classes are a Windows scheduling concept.
//
// Deprecated: moved to pkg/speechkit/stt/local.SetSubprocessPriorityLowered.
// This name is removed in v0.65.0; import the provider package instead.
func SetSubprocessPriorityLowered(bool) {}
