//go:build !cgo

// Package sherpa exposes the sherpa-onnx wake-word detector adapter.
package sherpa

import "github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"

var ErrCgoRequired = wakeword.ErrCgoRequired

type DetectorConfig = wakeword.DetectorConfig
type Detector = wakeword.Detector

var NewDetector = wakeword.NewDetector
