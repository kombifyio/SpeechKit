package procguard

import (
	"path"
	"sync/atomic"
)

// lowerPriorityDefault is the process-wide default consulted through
// [SubprocessPriorityLowered].
var lowerPriorityDefault atomic.Bool

func init() {
	lowerPriorityDefault.Store(true)
}

// SetSubprocessPriorityLowered sets the process-wide default for whether
// background subprocesses (whisper-server, the local LLM server, wake-word
// sidecars) are started at BELOW_NORMAL priority. It defaults to true and only
// has an effect on Windows. Hosts wire their performance opt-out here once.
func SetSubprocessPriorityLowered(lowered bool) {
	lowerPriorityDefault.Store(lowered)
}

// SubprocessPriorityLowered reports the process-wide default set by
// [SetSubprocessPriorityLowered].
func SubprocessPriorityLowered() bool {
	return lowerPriorityDefault.Load()
}

// BundleHelpersDir maps an executable path inside a macOS app bundle
// (…/SpeechKit.app/Contents/MacOS/SpeechKit) to the bundle's
// Contents/Helpers directory. It returns "" for a binary that does not live
// in a bundle, such as a `go build` output or a test binary. It is a pure
// path computation on slash-separated macOS paths and behaves the same on
// every platform.
func BundleHelpersDir(exe string) string {
	if exe == "" {
		return ""
	}
	macOSDir := path.Dir(exe)
	if path.Base(macOSDir) != "MacOS" {
		return ""
	}
	contents := path.Dir(macOSDir)
	if path.Base(contents) != "Contents" {
		return ""
	}
	return path.Join(contents, "Helpers")
}
