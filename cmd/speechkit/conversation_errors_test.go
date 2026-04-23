package main

import (
	"errors"
	"strings"
	"testing"
)

func TestFriendlyConversationErrorForLocalLLMConnectionRefused(t *testing.T) {
	err := errors.New(`assist: LLM failed: assist: all models failed: gemma4:e4b request: Post "http://127.0.0.1:8082/v1/chat/completions": dial tcp 127.0.0.1:8082: connectex: No connection could be made because the target machine actively refused it.`)

	message := friendlyConversationError(modeAssist, err)

	if strings.Contains(message, "Conversation failed") {
		t.Fatalf("message = %q, want specific local runtime guidance", message)
	}
	if !strings.Contains(message, "local Assist model runtime is not ready") {
		t.Fatalf("message = %q, want local Assist runtime guidance", message)
	}
}
