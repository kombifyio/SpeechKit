//go:build !windows

package procguard

import "os/exec"

// Adopt is a no-op outside Windows.
//
// The orphan class this package exists for is Windows-specific in practice:
// the desktop host is the only place that spawns these sidecars, and Windows
// has no parent-death signal. A POSIX equivalent (prctl PR_SET_PDEATHSIG on
// Linux, a process group plus a supervising kill on Darwin) can be added here
// if the sidecars ever ship in a non-Windows host.
func Adopt(_ *exec.Cmd) error { return nil }
