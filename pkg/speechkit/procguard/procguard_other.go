//go:build !windows && !darwin

package procguard

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Nothing here ties a child to this process, and nothing here pretends to.
//
// The orphan class this package exists for needs a host that spawns the
// sidecars, which today means the Windows and macOS desktop clients; the
// Linux build is the server, which starts at most one llama-server and is
// itself supervised by its container. Adopt used to return nil here, which
// read as "the child is now tied to us" — the same lie the macOS port had to
// unpick. A Linux equivalent (prctl PR_SET_PDEATHSIG, or the process-group
// scheme in procguard_darwin.go) belongs here when a Linux host ever spawns
// these children for real.

// Prepare is a no-op: there is nothing to arrange for a child that will not
// be adopted. It reports nothing, because Adopt is where the honest answer
// about this platform comes from.
func Prepare(*exec.Cmd) {}

// Adopt reports that this platform has no parent-death mechanism.
func Adopt(*exec.Cmd) error {
	return fmt.Errorf("%w: no child-process guard for %s", ErrUnsupportedPlatform, runtime.GOOS)
}

// Shutdown has nothing to terminate: Adopt never accepted a child.
func Shutdown() {}

// Sweep reports that stale children cannot be identified on this platform.
// Answering "0 stale processes found" would be indistinguishable from a clean
// machine, which is the whole failure this package was fixed for.
func Sweep(string) (int, error) {
	return 0, fmt.Errorf("%w: no stale-child sweep for %s", ErrUnsupportedPlatform, runtime.GOOS)
}
