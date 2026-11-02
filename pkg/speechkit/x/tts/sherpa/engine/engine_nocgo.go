//go:build !cgo

package engine

import "github.com/kombifyio/SpeechKit/pkg/speechkit/x/tts/sherpa"

// Engine is a loaded voice pair; without cgo none can be created.
type Engine struct{}

// New returns ErrCgoRequired: without cgo the sherpa-onnx runtime is not linked.
func New(string, int) (*Engine, error) {
	return nil, ErrCgoRequired
}

// Speak returns ErrCgoRequired.
func (*Engine) Speak(string, string, float32) ([]float32, int, string, error) {
	return nil, 0, "", ErrCgoRequired
}

// Close does nothing.
func (*Engine) Close() {}

// Compile-time check that Engine is a sherpa.Speaker.
var _ sherpa.Speaker = (*Engine)(nil)
