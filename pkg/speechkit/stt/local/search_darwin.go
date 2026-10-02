//go:build darwin

package local

import (
	"os"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/procguard"
)

// whisperBinaryNames returns the executable name searched on macOS. The
// bundle ships a static whisper.cpp build without an extension; a Windows
// ".exe" would not run here, so it is not probed.
func whisperBinaryNames() []string {
	return []string{"whisper-server"}
}

// platformWhisperSearchDirs returns the trusted managed-install candidates on
// macOS: only the app bundle's Contents/Helpers directory (where
// scripts/build-macos.sh places whisper-server, kombify-SpeechKit-mcos.11).
//
// Nothing outside the signed bundle is probed. A child of SpeechKit.app runs
// with SpeechKit's TCC grants (microphone, Accessibility) and is never
// prompted, and ~/Library/Application Support is writable by any process of
// the user, so a binary from there would inherit those grants. Homebrew and
// other PATH locations stay behind the explicit
// SPEECHKIT_ALLOW_WHISPER_PATH=1 developer escape hatch, as on Windows.
func platformWhisperSearchDirs() []string {
	exe, _ := os.Executable()
	if helpers := procguard.BundleHelpersDir(exe); helpers != "" {
		return []string{helpers}
	}
	return nil
}
