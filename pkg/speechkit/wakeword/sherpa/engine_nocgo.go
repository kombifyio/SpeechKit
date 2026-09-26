//go:build !cgo

package sherpa

// NewDetector returns ErrCgoRequired: without cgo the sherpa-onnx engine is
// not linked and nothing is registered with wakeword.RegisterEngine.
var NewDetector = func(DetectorConfig) (*Detector, error) {
	return nil, ErrCgoRequired
}
