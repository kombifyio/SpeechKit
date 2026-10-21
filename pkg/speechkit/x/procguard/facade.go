// Package procguard exposes the shared host child-process lifecycle and environment policy.
// Root SDK providers and x callers share one registry, priority setting and OS guard.
//
// Stability: Beta — API-checked; may change with a changelog callout.
package procguard

import (
	core "github.com/kombifyio/SpeechKit/pkg/speechkit/internal/procguard"
	"os/exec"
)

var ErrUnsupportedPlatform = core.ErrUnsupportedPlatform

type Process = core.Process

func Prepare(cmd *exec.Cmd)                                { core.Prepare(cmd) }
func Adopt(cmd *exec.Cmd) error                            { return core.Adopt(cmd) }
func AdoptOrReport(cmd *exec.Cmd, report func(error))      { core.AdoptOrReport(cmd, report) }
func Shutdown()                                            { core.Shutdown() }
func Sweep(root string) (int, error)                       { return core.Sweep(root) }
func SetSidecarEnvDrop(names []string)                     { core.SetSidecarEnvDrop(names) }
func HostSidecarEnv() []string                             { return core.HostSidecarEnv() }
func SidecarEnv(environ []string, drop ...string) []string { return core.SidecarEnv(environ, drop...) }
func SetSubprocessPriorityLowered(lowered bool)            { core.SetSubprocessPriorityLowered(lowered) }
func SubprocessPriorityLowered() bool                      { return core.SubprocessPriorityLowered() }
func BundleHelpersDir(exe string) string                   { return core.BundleHelpersDir(exe) }
func ConfigureHiddenProcess(cmd *exec.Cmd, lowered bool)   { core.ConfigureHiddenProcess(cmd, lowered) }
