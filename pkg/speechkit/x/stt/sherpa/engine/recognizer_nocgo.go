//go:build !cgo

package engine

import "github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/sherpa"

// Recognizer is a loaded model; without cgo none can be created.
type Recognizer struct{}

// NewRecognizer returns ErrCgoRequired: without cgo the sherpa-onnx runtime
// is not linked.
func NewRecognizer(sherpa.Config) (*Recognizer, error) {
	return nil, ErrCgoRequired
}

// Transcribe returns ErrCgoRequired.
func (*Recognizer) Transcribe([]float32) (sherpa.Result, error) {
	return sherpa.Result{}, ErrCgoRequired
}

// Close does nothing.
func (*Recognizer) Close() {}
