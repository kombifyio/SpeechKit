// Package sherpa is the sherpa-onnx keyword-spotting engine for
// pkg/speechkit/wakeword.
//
// Importing the package registers the engine with wakeword.RegisterEngine
// under EngineName, so a host that imports it (or calls NewDetector) gets a
// working wakeword.NewDetector and wakeword.NewPipeline. The exported surface
// is identical in every build: with cgo the engine drives the sherpa-onnx
// KeywordSpotter; with CGO_ENABLED=0 nothing is registered and NewDetector
// returns ErrCgoRequired, so hosts compile everywhere and discover the
// missing native engine at runtime.
package sherpa

import "github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"

// EngineName is the name the engine registers under. Set
// wakeword.DetectorConfig.Engine to it to select sherpa-onnx explicitly when
// more than one engine is registered; NewDetector does so itself.
const EngineName = "sherpa-onnx"

// ErrCgoRequired is returned by NewDetector in a build without cgo.
var ErrCgoRequired = wakeword.ErrCgoRequired

// DetectorConfig is the model configuration NewDetector accepts.
type DetectorConfig = wakeword.DetectorConfig

// Detector is the loaded model NewDetector returns.
type Detector = wakeword.Detector
