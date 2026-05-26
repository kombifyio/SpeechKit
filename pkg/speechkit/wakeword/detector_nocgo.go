//go:build !cgo

package wakeword

import "errors"

// ErrCgoRequired is returned by the no-cgo detector stub.
var ErrCgoRequired = errors.New("wakeword: sherpa-onnx KWS requires cgo build (set CGO_ENABLED=1)")

// DetectorConfig mirrors the cgo build's detector inputs.
type DetectorConfig struct {
	Encoder      string
	Decoder      string
	Joiner       string
	Tokens       string
	KeywordsFile string
	Keywords     []string
	NumThreads   int
	Threshold    float32
	Debug        bool
}

// Detector is the no-cgo placeholder for the cgo build's Detector.
type Detector struct{}

// Close is a no-op on the stub.
func (*Detector) Close() error { return nil }

// NewDetector returns ErrCgoRequired in the no-cgo build.
func NewDetector(_ DetectorConfig) (*Detector, error) {
	return nil, ErrCgoRequired
}
