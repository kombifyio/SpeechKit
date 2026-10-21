//go:build !windows

package procguard

import "os/exec"

// ConfigureHiddenProcess is a no-op outside Windows: POSIX hosts do not pop
// console windows for child processes, and priority classes are a Windows
// scheduling concept. It exists so callers need no build tags.
func ConfigureHiddenProcess(cmd *exec.Cmd, lowered bool) {
	_, _ = cmd, lowered
}
