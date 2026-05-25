// Package vad is the voice-activity-detection layer. The Silero ONNX
// path (silero.go) is currently a no-op stub pending the
// sherpa-onnx VAD migration; the RMS-level fallback (level_vad.go)
// arms the dictation silence-cutoff watcher in the meantime. The
// upstream Silero implementation will return when the ONNX runtime
// ABI clean-up lands.
//
// Audit 2026-05-24 maintainability sweep.
package vad
