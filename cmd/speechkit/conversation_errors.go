package main

import (
	"context"
	"errors"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func friendlyConversationError(cfg *config.Config, mode string, err error) string {
	if err == nil {
		return "Conversation failed. Check Settings and try again."
	}

	message := err.Error()
	lower := strings.ToLower(message)
	if strings.Contains(lower, "insufficient_quota") || strings.Contains(lower, "exceeded your current quota") {
		if mode == modeAssist || mode == modeVoiceAgent {
			return "The selected provider quota is exhausted. Configure a fallback model in Settings and try again."
		}
		return "The selected provider quota is exhausted. Check your provider billing and try again."
	}
	if errors.Is(err, context.Canceled) {
		return "Conversation was cancelled."
	}
	if strings.Contains(lower, "api key") || strings.Contains(lower, "token") || strings.Contains(lower, "credential") {
		return "A provider credential is missing or invalid. Check Settings > Provider."
	}
	if strings.Contains(lower, "model not found") {
		return "The selected model is unavailable. Check the model selection in Settings."
	}
	return "Conversation failed. Check Settings and try again."
}
