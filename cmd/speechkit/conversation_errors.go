package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func friendlyConversationError(mode string, err error) string {
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
	if strings.Contains(lower, "model not supported by provider") || strings.Contains(lower, "unsupported model") {
		return unsupportedModelConversationError(mode, message, lower)
	}
	if strings.Contains(lower, "model not found") {
		return "The selected model is unavailable. Check the model selection in Settings."
	}
	return "Conversation failed. Check Settings and try again."
}

func unsupportedModelConversationError(mode, message, lower string) string {
	provider := "this provider"
	if strings.Contains(lower, "hf-inference") || strings.Contains(lower, "huggingface") {
		provider = "Hugging Face Inference"
	}

	modelLabel := "model"
	switch mode {
	case modeAssist:
		modelLabel = "Assist model"
	case modeVoiceAgent:
		modelLabel = "Voice Agent model"
	}

	if model := failedConversationModelName(message); model != "" {
		return fmt.Sprintf("The selected %s %s is not supported by %s. Choose another model in Settings > Models or configure a different provider.", modelLabel, model, provider)
	}
	return fmt.Sprintf("The selected %s is not supported by %s. Choose another model in Settings > Models or configure a different provider.", modelLabel, provider)
}

func failedConversationModelName(message string) string {
	idx := strings.LastIndex(message, " error (")
	if idx < 0 {
		return ""
	}
	prefix := strings.NewReplacer(":", " ", "\n", " ", "\t", " ").Replace(message[:idx])
	fields := strings.Fields(prefix)
	for i := len(fields) - 1; i >= 0; i-- {
		candidate := strings.Trim(fields[i], `"'(),[]{}.`)
		if strings.Contains(candidate, "/") {
			return candidate
		}
	}
	return ""
}
