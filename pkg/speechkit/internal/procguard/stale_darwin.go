//go:build darwin

package procguard

import (
	"path/filepath"
	"strings"
)

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
