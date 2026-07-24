//go:build windows

package stt

import (
	"os/exec"
	"sync/atomic"
	"syscall"
)

const createNoWindow = 0x08000000

// belowNormalPriorityClass starts the whisper-server subprocess at
// BELOW_NORMAL_PRIORITY_CLASS. On an idle machine priority is irrelevant
// (the child still gets every free core); under contention it guarantees
// the live capture pipeline in the host process preempts transcription
// instead of being starved by it.
const belowNormalPriorityClass = 0x00004000

// lowerPriority is on by default; hosts can opt out via
// [SetSubprocessPriorityLowered] (config knob).
var lowerPriority atomic.Bool

func init() {
	lowerPriority.Store(true)
}

// SetSubprocessPriorityLowered toggles whether STT subprocesses are
// spawned at BELOW_NORMAL priority (default true). No-op on non-Windows.
func SetSubprocessPriorityLowered(lowered bool) {
	lowerPriority.Store(lowered)
}

func configureHiddenProcess(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	flags := uint32(createNoWindow)
	if lowerPriority.Load() {
		flags |= belowNormalPriorityClass
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: flags,
	}
}
