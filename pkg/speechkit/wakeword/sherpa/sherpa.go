// Package sherpa exposes the sherpa-onnx wake-word detector adapter.
//
// The adapter has the same exported surface in every build. With cgo it
// drives the sherpa-onnx KeywordSpotter; with CGO_ENABLED=0 its constructors
// return [ErrCgoRequired] so hosts compile everywhere and discover the
// missing native detector at runtime.
package sherpa

import "github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"

// ErrCgoRequired is returned by NewDetector in a build without cgo.
var ErrCgoRequired = wakeword.ErrCgoRequired

type DetectorConfig = wakeword.DetectorConfig
type Detector = wakeword.Detector

var NewDetector = wakeword.NewDetector
