//go:build !cgo

package wakeword_test

import (
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword/sherpa"
)

type discardSink struct{}

func (discardSink) Emit(wakeword.DetectionEvent) {}

// A CGO_ENABLED=0 host must compile against the full detector/pipeline
// surface and learn about the missing native engine only at runtime.
func TestNoCgoBuildFailsClosedWithErrCgoRequired(t *testing.T) {
	det, err := sherpa.NewDetector(sherpa.DetectorConfig{Keywords: []string{"hey speechkit"}})
	if !errors.Is(err, wakeword.ErrCgoRequired) {
		t.Fatalf("NewDetector err = %v, want ErrCgoRequired", err)
	}
	if det != nil {
		t.Fatal("NewDetector returned a detector without cgo")
	}

	pipe, err := wakeword.NewPipeline(det, discardSink{}, wakeword.Config{})
	if !errors.Is(err, wakeword.ErrCgoRequired) {
		t.Fatalf("NewPipeline err = %v, want ErrCgoRequired", err)
	}
	if pipe != nil {
		t.Fatal("NewPipeline returned a pipeline without cgo")
	}
}
