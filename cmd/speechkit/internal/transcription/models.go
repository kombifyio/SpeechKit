// Package transcription provides STT model-selection helpers and the
// vocabulary-dictionary primitives that the desktop adapters consume.
//
// This package is intentionally a leaf: it only depends on
// internal/config and internal/store, plus stdlib. It contains no
// references to the cmd/speechkit appState type and no Wails-specific
// code, so it is safe to import from any subpackage of cmd/speechkit
// without import cycles.
package transcription

import (
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
)

// ConfiguredModelHints returns a per-provider map of currently configured
// transcription models, used for surfacing the active model in the UI
// and for telemetry hints.
func ConfiguredModelHints(cfg *config.Config) map[string]string {
	if cfg == nil {
		return nil
	}

	hints := map[string]string{}
	if config.ManagedHuggingFaceAvailableInBuild() {
		if model := strings.TrimSpace(cfg.HuggingFace.Model); model != "" {
			hints["huggingface"] = model
			hints["hf"] = model
		}
	}
	if model := ConfiguredLocalModel(cfg); model != "" {
		hints["local"] = model
	}
	return hints
}

// ConfiguredLocalModel returns the basename of the local whisper.cpp
// model the user has selected, or the model alias if no path is set.
func ConfiguredLocalModel(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if modelPath := strings.TrimSpace(cfg.Local.ModelPath); modelPath != "" {
		return filepath.Base(modelPath)
	}
	return strings.TrimSpace(cfg.Local.Model)
}
