package capture_test

import (
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio/capture"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/pipeline"
)

// A capture Session is the framework's production AudioRecorder: any
// value satisfying capture.Session must satisfy the kernel's recorder
// contracts, including the pooled-buffer optimisation.
var (
	_ speechkit.AudioRecorder    = capture.Session(nil)
	_ pipeline.PooledPCMRecorder = capture.Session(nil)
)
