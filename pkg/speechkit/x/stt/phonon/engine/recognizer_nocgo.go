//go:build !cgo || !(amd64 || arm64)

package engine

import (
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/phonon"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/sherpa"
)

// Recognizer is a loaded model; this build cannot create one.
type Recognizer struct{}

// NewRecognizer returns [ErrCgoRequired]: this build does not link the engine.
func NewRecognizer(phonon.Config) (*Recognizer, error) { return nil, ErrCgoRequired }

// Transcribe returns [ErrCgoRequired].
func (*Recognizer) Transcribe([]float32) (sherpa.Result, error) {
	return sherpa.Result{}, ErrCgoRequired
}

// Close does nothing.
func (*Recognizer) Close() {}
