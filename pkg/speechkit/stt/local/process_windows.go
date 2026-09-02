//go:build windows

package local

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

// lowerPriority is the process-wide default (on); hosts can opt out via
// [SetSubprocessPriorityLowered] or per instance via
// [Provider.LowerSubprocessPriority].
var lowerPriority atomic.Bool

func init() {
	lowerPriority.Store(true)
}

// SetSubprocessPriorityLowered toggles the process-wide default for whether
// STT subprocesses are spawned at BELOW_NORMAL priority (default true). No-op
// on non-Windows. Providers with an explicit LowerSubprocessPriority ignore
// it.
func SetSubprocessPriorityLowered(lowered bool) {
	lowerPriority.Store(lowered)
}

// subprocessPriorityLowered resolves the effective setting for a provider.
func subprocessPriorityLowered(override *bool) bool {
	if override != nil {
		return *override
	}
	return lowerPriority.Load()
}

func configureHiddenProcess(cmd *exec.Cmd, lowered bool) {
	if cmd == nil {
		return
	}
	flags := uint32(createNoWindow)
	if lowered {
		flags |= belowNormalPriorityClass
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: flags,
	}
}
