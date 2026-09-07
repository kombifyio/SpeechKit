// Package procguard ties long-lived child processes to the lifetime of the
// process that spawned them.
//
// SpeechKit's CPU-heavy children — the wake-word sidecars, whisper-server and
// the local LLM server — are deliberately rooted in a background context so a
// short-lived HTTP request context cannot kill them (see
// cmd/speechkit/desktop_wakeword.go for that bug history). Their shutdown is
// owned by the host's own Close paths and the startup cleanup stack.
//
// That covers every ORDERLY exit. It does not cover a crash, a taskkill, or a
// dev-loop rebuild that replaces the host binary: those skip the cleanup stack
// entirely and leave the children running forever. Each orphan holds on the
// order of a gigabyte of commit charge, so a day of restarts can exhaust the
// system commit limit while physical memory still looks healthy — at which
// point unrelated tools start failing to allocate (observed 2026-08-27:
// 6 orphans, ~7 GB, commit limit down to 0.4 GB free of 47.9 GB).
//
// Adopt closes that gap by handing the child to the operating system: the OS
// terminates it when the parent goes away, however the parent goes away.
//
// # How each platform keeps that promise
//
// Windows has a Job Object with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, which is
// a real parent-death guarantee: the kernel kills the children when the last
// job handle closes, and that happens even on an unhandled crash. [Prepare]
// is a no-op there and [Sweep] finds nothing, because nothing can be left.
//
// macOS has no such primitive, so the guarantee is assembled from three
// weaker parts and is honest about being weaker: [Prepare] puts each child in
// its own process group before it starts, [Shutdown] kills those groups
// (SIGTERM, then SIGKILL) from the host's cleanup path and from a
// SIGTERM/SIGINT handler, and [Sweep] terminates whatever an earlier run
// still left behind. A kill -9 of the host defeats all three; the next start
// sweeps the remains.
//
// Everywhere else [Adopt] and [Sweep] report [ErrUnsupportedPlatform]. They
// used to return nil, which told a caller that a child was tied to the host
// when nothing of the sort had happened.
//
// The call order is [Prepare] before cmd.Start, [Adopt] straight after it.
// Both are robustness measures rather than preconditions: a child that could
// not be adopted still works, so callers log and carry on.
package procguard

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrUnsupportedPlatform is returned by [Adopt] and [Sweep] on a platform
// with no mechanism to tie a child to this process.
//
// It exists because the previous no-op returned nil: a caller could not tell
// "the child is now tied to us" from "nothing happened". On macOS that
// difference is a gigabyte of leftover whisper-server after every crash, so
// the honest answer has to be an error even though the caller only logs it.
var ErrUnsupportedPlatform = errors.New("procguard: child processes cannot be tied to this process on this platform")

// Process is one running process, as the platform reports it to [Sweep].
type Process struct {
	// PID is the process identifier.
	PID int
	// ExecutablePath is the absolute path of the running image. It is the
	// only field that can prove a process is ours, which is why the sweep
	// never looks at the process name.
	ExecutablePath string
}

// staleUnder picks the processes whose executable image lives inside root.
//
// Matching on the executable path, and only on it, is the whole safety
// argument of the sweep. A match by name would find the user's own
// whisper-server — a Homebrew build, a checkout they compiled themselves —
// and kill it. A process running out of our own application bundle can only
// have been started by a copy of this application.
//
// self is excluded so a host that happens to live under the same root (the
// bundle's Contents/MacOS is a sibling of Contents/Helpers, and a caller may
// pass a wider root) can never sweep itself. PID 0 and 1 are excluded
// because they are the kernel and launchd.
func staleUnder(processes []Process, root string, self int) []Process {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	var stale []Process
	for _, process := range processes {
		if process.PID <= 1 || process.PID == self {
			continue
		}
		if !pathIsUnder(root, process.ExecutablePath) {
			continue
		}
		stale = append(stale, process)
	}
	return stale
}

// pathIsUnder reports whether candidate names a file inside root.
//
// Comparison is exact and case-sensitive on the cleaned paths, with no
// symlink resolution. That is deliberately the strict direction: a path that
// should have matched but did not leaves an orphan behind, which is visible
// and recoverable, while a path that matched but should not have kills a
// process that belongs to someone else.
func pathIsUnder(root, candidate string) bool {
	root = strings.TrimSpace(root)
	candidate = strings.TrimSpace(candidate)
	if root == "" || candidate == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
