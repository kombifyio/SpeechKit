//go:build darwin && cgo

package capture

import "github.com/gen2brain/malgo"

// macOS seam of the shared malgo capture session (capture_malgo_cgo.go):
// the CoreAudio backend name and the malgo context backend list.
// Microphone capture runs the very same session as Windows; system
// loopback has no CoreAudio equivalent in miniaudio and waits for the
// process-tap slice (kombify-SpeechKit-mcos.16).

func init() {
	if err := RegisterBackend(BackendDarwinCoreAudioMalgo, newMalgoSession); err != nil {
		panic(err)
	}
}

// platformMalgoBackend names the backend the shared malgo session registers
// as and reports in its events on this platform.
func platformMalgoBackend() Backend {
	return BackendDarwinCoreAudioMalgo
}

// platformMalgoContextBackends is the malgo backend list the session and the
// device enumeration initialise their context with.
func platformMalgoContextBackends() []malgo.Backend {
	return []malgo.Backend{malgo.BackendCoreaudio}
}

// platformSupportsLoopback reports whether InputSourceSystemLoopback can be
// opened. CoreAudio has no loopback capture device, so newMalgoSession
// refuses the system-audio sources before a context exists.
func platformSupportsLoopback() bool {
	return false
}

// ensureLoopbackOutputDeviceAvailable is never reached on macOS because
// newMalgoSession refuses every loopback source up front; it exists so the
// shared Start path compiles and fails closed should that guard ever move.
func ensureLoopbackOutputDeviceAvailable(cfg Config) error {
	return errSourceUnavailableOnPlatform(cfg.InputSource)
}
