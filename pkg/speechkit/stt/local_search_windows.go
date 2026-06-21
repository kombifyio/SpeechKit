//go:build windows

package stt

import (
	"os"
	"path/filepath"
)

// whisperBinaryNames returns the executable names searched on Windows.
// Only the .exe variant is meaningful here.
func whisperBinaryNames() []string {
	return []string{"whisper-server.exe"}
}

// platformWhisperSearchDirs returns the managed-install candidates the
// Wails Device-Target installer drops the whisper runtime into. The
// installer (installer/speechkit.nsi) writes to %LOCALAPPDATA%\SpeechKit
// or its bin/ subdirectory so the bundle and the managed runtime live
// next to each other.
func platformWhisperSearchDirs() []string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return nil
	}
	return []string{
		filepath.Join(localAppData, "SpeechKit"),
		filepath.Join(localAppData, "SpeechKit", "bin"),
	}
}
