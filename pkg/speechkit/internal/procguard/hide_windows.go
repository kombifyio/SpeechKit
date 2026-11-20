//go:build windows

package procguard

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// belowNormalPriorityClass is BELOW_NORMAL_PRIORITY_CLASS. On an idle machine
// priority is irrelevant (the child still gets every free core); under
// contention it guarantees the host's live capture pipeline preempts the
// child instead of being starved by it.
const belowNormalPriorityClass = 0x00004000

// aboveNormalPriorityClass is ABOVE_NORMAL_PRIORITY_CLASS.
const aboveNormalPriorityClass = 0x00008000

// ConfigureHiddenProcessRaised makes cmd start without a console window at
// ABOVE_NORMAL priority: for an engine the person waits on. A speech engine
// that runs its threads in lock step (the Phonon decoder's fork-join pool) stalls
// for the whole time slice whenever one of its threads is preempted, so at
// NORMAL priority next to a busy machine a decode of seconds of speech took
// minutes (measured 63 times slower with every core busy; at ABOVE_NORMAL the
// same decode ran at idle speed). It replaces cmd.SysProcAttr.
func ConfigureHiddenProcessRaised(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: uint32(createNoWindow) | aboveNormalPriorityClass,
	}
}

// ConfigureHiddenProcess makes cmd start without a console window on Windows
// and, when lowered is true, at BELOW_NORMAL priority. It replaces
// cmd.SysProcAttr, so call it before setting other attributes. A nil cmd is
// ignored. It is a no-op on other platforms.
func ConfigureHiddenProcess(cmd *exec.Cmd, lowered bool) {
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
