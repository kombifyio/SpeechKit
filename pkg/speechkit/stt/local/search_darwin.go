//go:build darwin

package local

import (
	"os"
	"path/filepath"
)

// whisperBinaryNames returns the executable name searched on macOS. The
// bundle ships a static whisper.cpp build without an extension; a Windows
// ".exe" would not run here, so it is not probed.
func whisperBinaryNames() []string {
	return []string{"whisper-server"}
}

// platformWhisperSearchDirs returns the trusted managed-install candidates on
// macOS: the app bundle's Contents/Helpers directory (where
// scripts/build-macos.sh places whisper-server, kombify-SpeechKit-mcos.11)
// and the per-user Application Support directory the desktop uses for
// downloaded runtimes. Homebrew and other PATH locations are deliberately
// not probed: as on Windows, a binary from PATH is only used behind the
// explicit SPEECHKIT_ALLOW_WHISPER_PATH=1 escape hatch.
func platformWhisperSearchDirs() []string {
	var dirs []string
	exe, _ := os.Executable()
	if helpers := bundleHelpersDirFor(exe); helpers != "" {
		dirs = append(dirs, helpers)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		base := filepath.Join(home, "Library", "Application Support", "SpeechKit")
		dirs = append(dirs, base, filepath.Join(base, "bin"))
	}
	return dirs
}

// bundleHelpersDirFor maps an executable path inside a macOS app bundle
// (…/SpeechKit.app/Contents/MacOS/SpeechKit) to the bundle's
// Contents/Helpers directory. It returns "" for a binary that does not live
// in a bundle, such as a `go build` output or the test binary.
func bundleHelpersDirFor(exe string) string {
	if exe == "" {
		return ""
	}
	macOSDir := filepath.Dir(exe)
	if filepath.Base(macOSDir) != "MacOS" {
		return ""
	}
	contents := filepath.Dir(macOSDir)
	if filepath.Base(contents) != "Contents" {
		return ""
	}
	return filepath.Join(contents, "Helpers")
}
