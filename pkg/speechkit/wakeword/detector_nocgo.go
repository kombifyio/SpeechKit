//go:build !cgo

package wakeword

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

// Threshold always reports 0 on the stub.
func (*Detector) Threshold() float32 { return 0 }

// Close is a no-op on the stub.
func (*Detector) Close() error { return nil }

// NewDetector returns ErrCgoRequired in the no-cgo build.
func NewDetector(_ DetectorConfig) (*Detector, error) {
	return nil, ErrCgoRequired
}
