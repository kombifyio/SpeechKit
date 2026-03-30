package main

import (
	"fmt"
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
	modelPath := defaultDictationVADModelPath()
	if modelPath == "" {
		return nil, nil, nil
	}

	detector, err := vad.NewSileroVAD(modelPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load dictation vad: %w", err)
	}

	return detector, detector.Close, nil
}
