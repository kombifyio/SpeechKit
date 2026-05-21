//go:build !cgo

package wakeword

import "errors"

// ErrCgoRequired is returned by the no-cgo detector stub. The sherpa-onnx
// KWS engine ships as a native shared library accessed via CGo
// (github.com/k2-fsa/sherpa-onnx-go); a CGo-disabled build cannot load it
// and falls back to this stub. Callers that need wake-word must set
// CGO_ENABLED=1 and build against the bundled MinGW-compiled libs.
var ErrCgoRequired = errors.New("wakeword: sherpa-onnx KWS requires cgo build (set CGO_ENABLED=1)")

// DetectorConfig mirrors the cgo build's assembled set of paths needed by
// the sherpa-onnx KeywordSpotter.
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

// Detector is the no-cgo placeholder for the cgo build's Detector struct.
// Construction always fails with ErrCgoRequired so dependent packages
// compile but cannot create a working wake-word pipeline.
type Detector struct{}

// Close is a no-op on the stub.
func (*Detector) Close() error { return nil }

// NewDetector returns ErrCgoRequired in the no-cgo build.
func NewDetector(_ DetectorConfig) (*Detector, error) {
	return nil, ErrCgoRequired
}
