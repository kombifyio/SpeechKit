//go:build cgo

package sherpa_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword/sherpa"
)

// Importing the package registers the engine: both constructors reach the
// model-path validation instead of failing with ErrEngineUnavailable or
// ErrCgoRequired.
func TestCgoBuildRegistersEngine(t *testing.T) {
	for name, construct := range map[string]func() error{
		"sherpa.NewDetector": func() error {
			_, err := sherpa.NewDetector(sherpa.DetectorConfig{})
			return err
		},
		"wakeword.NewDetector": func() error {
			_, err := wakeword.NewDetector(wakeword.DetectorConfig{Engine: sherpa.EngineName})
			return err
		},
	} {
		err := construct()
		if err == nil {
			t.Fatalf("%s with an empty config succeeded", name)
		}
		if errors.Is(err, wakeword.ErrEngineUnavailable) || errors.Is(err, sherpa.ErrCgoRequired) {
			t.Fatalf("%s err = %v, want the model path validation error", name, err)
		}
		if !strings.Contains(err.Error(), "paths all required") {
			t.Fatalf("%s err = %v, want the model path validation error", name, err)
		}
	}
}
