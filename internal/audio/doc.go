// Package audio holds the reference app's private audio playback layer
// (oto/malgo players for TTS and voice-agent output) plus compatibility
// re-exports: PCM helpers forward to pkg/speechkit/audio (pcm_compat.go)
// and the capture layer forwards to pkg/speechkit/audio/capture
// (shim.go), so existing call sites keep using the audio.* names
// unchanged. New capture code goes in the public capture package.
package audio
