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
