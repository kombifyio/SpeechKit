//go:build windows || darwin

package procguard

import "os/exec"

// AdoptOrReport hands cmd to [Adopt] and passes a failure to report.
//
// Callers treat adoption as a robustness measure, not a precondition: a child
// that could not be adopted still works, so they log and carry on. This
// helper is that call shape, split per platform so the caller does not
// compare an error that, on a platform without a guard, can never be nil.
// report is not called when the child was adopted.
func AdoptOrReport(cmd *exec.Cmd, report func(error)) {
	if err := Adopt(cmd); err != nil {
		report(err)
	}
}
