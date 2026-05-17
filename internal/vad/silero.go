//go:build cgo

package vad

import (
	"errors"
	"sync"
)

// NOTE — historical context:
//
// This file previously embedded the Silero VAD via github.com/yalue/onnxruntime_go.
// Linking that binding into the SpeechKit binary alongside github.com/k2-fsa/sherpa-onnx-go
// (which is used by internal/wakeword) caused a hard native abort on Windows
// during wake-word initialisation — both modules link `-lonnxruntime` with
// different ABI expectations (MSVC vs MinGW), and the resulting symbol
// collisions corrupted process state once one of the two engines tried to
// start its runtime.
//
// VAD is opt-in and has historically not been bundled on the Windows
// reference target (silero_vad.onnx is not shipped). The dictation flow
// works fine without it — push-to-talk ends when the user releases the
// hotkey. Until we re-implement VAD on top of sherpa-onnx's own
// `sherpa.VoiceActivityDetector`, this stub keeps the package surface
// intact and reports "VAD unavailable" if anyone tries to instantiate.

const (
	SampleRate     = 16000
	FrameSize      = 512
	BytesPerSample = 2
	FrameBytes     = FrameSize * BytesPerSample
)

// ErrUnavailable is returned by NewSileroVAD until VAD is re-implemented on
// the unified sherpa-onnx runtime. See package-level note for rationale.
var ErrUnavailable = errors.New("vad: silero VAD temporarily unavailable on this build — tracked in beads (replace yalue/onnxruntime_go usage with sherpa-onnx VAD to avoid double-runtime conflict)")

// Detector is the speech-probability contract consumed by dictation processors.
type Detector interface {
	ProcessFrame([]int16) (float32, error)
	Reset()
}

// SileroVAD is the public type the package previously exported. Kept as an
// empty struct so callers compile; constructor returns ErrUnavailable.
type SileroVAD struct {
	//lint:ignore U1000 reserved for the sherpa-VAD reimplementation that will reuse the lock around buffered ProcessFrame state.
	mu sync.Mutex
}

// NewSileroVAD always returns ErrUnavailable in this build. Adapters
// (cmd/speechkit/dictation_session.go) detect the error, log a warning and
// degrade to manual-release hotkey behaviour.
func NewSileroVAD(_ string) (*SileroVAD, error) {
	return nil, ErrUnavailable
}

// ProcessFrame is a no-op for the stub. The real implementation will return
// the speech probability from the sherpa-onnx VAD model.
func (v *SileroVAD) ProcessFrame(_ []int16) (float32, error) {
	return 0, ErrUnavailable
}

// Reset is a no-op for the stub.
func (v *SileroVAD) Reset() {}

// Close is a no-op for the stub.
func (v *SileroVAD) Close() {}
