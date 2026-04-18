package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

func validateLocalProviderActivation(cfg *config.Config, modelPath string) error {
	if strings.TrimSpace(modelPath) == "" {
		return errors.New("whisper model path missing")
	}
	if err := stt.ValidateModelPath(modelPath); err != nil {
		return err
	}

	port := 0
	gpu := ""
	if cfg != nil {
		port = cfg.Local.Port
		gpu = cfg.Local.GPU
	}

	status := stt.NewLocalProvider(port, modelPath, gpu).VerifyInstallation()
	if !status.BinaryFound {
		return errors.New("whisper-server binary missing")
	}
	if status.ModelFound {
		return nil
	}

	for _, problem := range status.Problems {
		if strings.TrimSpace(problem) != "" {
			return fmt.Errorf("whisper model invalid: %s", problem)
		}
	}
	return errors.New("whisper model missing")
}
