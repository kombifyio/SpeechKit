package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/vad"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func newDictationCaptureSession(detector vad.Detector, cfg *config.Config) *speechkit.DictationSegmenter {
	if detector == nil {
		return nil
	}

	pauseThreshold := 700 * time.Millisecond
	if cfg != nil && cfg.General.AutoStopSilenceMs > 0 {
		pauseThreshold = time.Duration(cfg.General.AutoStopSilenceMs) * time.Millisecond
	}

	return speechkit.NewDictationSegmenter(detector, pauseThreshold)
}

func defaultDictationVADModelPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}

	modelPath := filepath.Join(filepath.Dir(exe), "silero_vad.onnx")
	if _, err := os.Stat(modelPath); err != nil {
		return ""
	}
	return modelPath
}

func newDictationVAD() (vad.Detector, func(), error) {
	// Prefer Silero when a model is bundled AND the cgo-backed binding
	// can construct a detector. The Silero binding is currently disabled
	// (returns ErrUnavailable) because its onnxruntime conflicts with the
	// sherpa-onnx runtime — see internal/vad/silero.go for the full note.
	// Until the planned re-implementation on sherpa-onnx VAD lands, fall
	// through to the level-based detector below so dictation auto-stop
	// remains functional instead of silently broken.
	if modelPath := defaultDictationVADModelPath(); modelPath != "" {
		if detector, err := vad.NewSileroVAD(modelPath); err == nil {
			return detector, detector.Close, nil
		}
	}

	// Fallback: RMS-level detector. No ONNX dependency, no model file.
	// Less precise than Silero (keyboard noise can register as speech)
	// but it satisfies the IdleObserver contract so the silence-cutoff
	// watcher actually arms and the dictate session auto-stops on quiet.
	level := vad.NewLevelVAD()
	return level, func() {}, nil
}
