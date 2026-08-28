// Package local is the built-in whisper.cpp provider for SpeechKit: a
// subprocess the host starts and stops, so transcription never leaves the
// machine.
//
// In v0.64.0 the identifiers below alias the implementation that still lives
// in pkg/speechkit/stt. v0.65.0 moves the implementation here and deletes the
// old names, so code written against this package needs no further change.
package local

//lint:file-ignore SA1019 This package exists to alias the names deprecated in
// pkg/speechkit/stt; flagging its own aliases would defeat the migration window.

import "github.com/kombifyio/SpeechKit/pkg/speechkit/stt"

// Provider transcribes through a host-managed whisper.cpp server.
type Provider = stt.LocalProvider

// InstallStatus reports whether the binary and model a Provider needs are
// present and usable.
type InstallStatus = stt.InstallStatus

// MinWhisperModelBytes is the smallest file size accepted as a real model;
// anything smaller is a truncated or failed download.
const MinWhisperModelBytes = stt.MinWhisperModelBytes

// New returns a whisper.cpp provider. The process is not started; lifecycle
// stays with the host.
func New(port int, modelPath, gpu string) *Provider {
	return stt.NewLocalProvider(port, modelPath, gpu)
}

// ValidateModelPath verifies that path points at a whisper.cpp ggml model file
// with a safe filename: absolute, no traversal, ggml-*.bin.
func ValidateModelPath(path string) error { return stt.ValidateModelPath(path) }

// FindWhisperBinary locates the whisper-server executable without starting it,
// so a host can report runtime readiness.
func FindWhisperBinary() (string, error) { return stt.FindWhisperBinary() }

// SetSubprocessPriorityLowered toggles whether the whisper.cpp subprocess is
// spawned at below-normal priority (default true). No-op outside Windows.
func SetSubprocessPriorityLowered(lowered bool) { stt.SetSubprocessPriorityLowered(lowered) }
