//go:build !cgo

package sherpa_test

import (
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword/sherpa"
)

// A CGO_ENABLED=0 host must compile against the full detector surface and
// learn about the missing native engine only at runtime.
func TestNoCgoBuildFailsClosedWithErrCgoRequired(t *testing.T) {
	det, err := sherpa.NewDetector(sherpa.DetectorConfig{Keywords: []string{"hey speechkit"}})
	if !errors.Is(err, sherpa.ErrCgoRequired) || !errors.Is(err, wakeword.ErrCgoRequired) {
		t.Fatalf("NewDetector err = %v, want ErrCgoRequired", err)
	}
	if det != nil {
		t.Fatal("NewDetector returned a detector without cgo")
	}

	// Nothing registered the engine, so the root constructor fails closed too.
	_, err = wakeword.NewDetector(wakeword.DetectorConfig{Engine: sherpa.EngineName})
	if !errors.Is(err, wakeword.ErrEngineUnavailable) {
		t.Fatalf("wakeword.NewDetector err = %v, want ErrEngineUnavailable", err)
	}
}
