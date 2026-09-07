// Package capture is the public microphone / system-audio capture layer
// of the SpeechKit framework: backend registry ([RegisterBackend], [Open]),
// capture [Session] contract, device enumeration ([ListCaptureDevices],
// [ListOutputDevices]), and the pooled PCM frame buffers ([FramePool]).
//
// Backends today: one malgo (miniaudio) capture session shared by the
// Windows WASAPI backend ([BackendWindowsWASAPIMalgo]) and the macOS
// CoreAudio backend ([BackendDarwinCoreAudioMalgo]), compiled in behind
// the "(windows || darwin) && cgo" build tags with a small per-platform
// seam. System loopback ([InputSourceSystemLoopback]) is Windows-only
// until the macOS process-tap slice lands; the CoreAudio backend reports
// [ErrUnsupportedSource] for it at construction. On every other build
// (other platforms, or CGO_ENABLED=0) [Open] and [NewCapturer] return an
// error wrapping [ErrBackendUnavailable]. [RegisterBackend] is the
// extension point for additional platform backends.
//
// Captured PCM uses the canonical SpeechKit format (16 kHz, 16-bit
// signed, mono) declared in pkg/speechkit/audio; a [Session] structurally
// satisfies pkg/speechkit's AudioRecorder (and, via SetPooledPCMHandler,
// its PooledPCMRecorder optimisation).
//
// Playback (TTS/voice-agent output) is intentionally not part of this
// package; the reference app keeps it in internal/audio.
package capture
