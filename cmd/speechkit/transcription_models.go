package main

import (
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func configuredTranscriptionModelHints(cfg *config.Config) map[string]string {
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
	if model := configuredLocalTranscriptionModel(cfg); model != "" {
		hints["local"] = model
	}
	return hints
}

func configuredLocalTranscriptionModel(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if modelPath := strings.TrimSpace(cfg.Local.ModelPath); modelPath != "" {
		return filepath.Base(modelPath)
	}
	return strings.TrimSpace(cfg.Local.Model)
}
