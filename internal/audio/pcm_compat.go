package audio

// PCM format constants and helpers now live in the public pkg/speechkit/audio
// package so promoted STT/TTS adapters (under pkg/speechkit/**) can share them
// without importing internal/. internal/audio re-exports them here so the
// device-capture code (incl. the //go:build windows && cgo files) and existing
// callers keep using audio.SampleRate / audio.PCMToWAV unchanged.

import audiopkg "github.com/kombifyio/SpeechKit/pkg/speechkit/audio"

// Canonical capture PCM format: 16 kHz, 16-bit signed, mono.
const (
	SampleRate     = audiopkg.SampleRate
	Channels       = audiopkg.Channels
	BitsPerSample  = audiopkg.BitsPerSample
	BytesPerSample = audiopkg.BytesPerSample
)

// PCM helpers (function values so const/var callers and the cgo capture code
// keep resolving audio.PCMToWAV etc. without edits).
var (
	PCMToWAV        = audiopkg.PCMToWAV
	PCMDurationSecs = audiopkg.PCMDurationSecs
	PCMLevel        = audiopkg.PCMLevel
)
