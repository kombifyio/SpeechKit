//go:build !windows

package local

// whisperBinaryNames returns the executable names searched on
// Linux/macOS. Both the bare and .exe variants are accepted so a
// developer using a Windows-built bundle on a Unix host can still
// find the runtime.
func whisperBinaryNames() []string {
	return []string{"whisper-server", "whisper-server.exe"}
}

// platformWhisperSearchDirs returns no extra managed-install candidates
// on Unix. The Server-Target Linux container reads cfg.VPS.WhisperBinary
// (settable via TOML or PATCH /v1/server/settings) for the whisper path,
// so the kernel doesn't probe filesystem locations on Linux.
func platformWhisperSearchDirs() []string {
	return nil
}
