package netsec

import (
	"net/http"
	"strings"
	"testing"
)

func TestProviderStatusErrorDoesNotLeakResponseBody(t *testing.T) {
	body := []byte(`{"error":{"message":"invalid api key sk-secret-123"}}`)

	err := ProviderStatusError("openai", http.StatusUnauthorized, body)
	if err == nil {
		t.Fatal("expected error")
	}
	message := err.Error()
	if strings.Contains(message, "sk-secret") || strings.Contains(message, "invalid api key") {
		t.Fatalf("provider body leaked in error: %q", message)
	}
	if !strings.Contains(message, "openai error (401): provider authentication failed") {
		t.Fatalf("unexpected error message: %q", message)
	}
}

func TestSafeProviderErrorReasonClassifiesKnownFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"rate limit", http.StatusTooManyRequests, `{"error":"anything"}`, "provider rate limit exceeded"},
		// llama-server, 2026-09-03: a 21-minute transcript segment in one
		// request. The word "tokens" made this an authentication failure.
		{"llama-server context size", http.StatusBadRequest, `{"error":{"code":400,"message":"request (9016 tokens) exceeds the available context size (4096 tokens), try increasing it","type":"exceed_context_size_error","n_prompt_tokens":9016,"n_ctx":4096}}`, "provider context limit exceeded"},
		{"openai context length", http.StatusBadRequest, `{"error":{"message":"This model's maximum context length is 8192 tokens. However, your messages resulted in 9000 tokens."}}`, "provider context limit exceeded"},
		{"max_tokens is not a credential", http.StatusBadRequest, `{"error":"max_tokens must be a positive integer"}`, "provider rejected request"},
		{"expired token", http.StatusBadRequest, `{"error":"token expired"}`, "provider authentication failed"},
		{"unauthorized", http.StatusUnauthorized, `{"error":"nope"}`, "provider authentication failed"},
		{"quota", http.StatusBadRequest, `{"code":"insufficient_quota"}`, "provider quota exhausted"},
		{"unsupported model", http.StatusBadRequest, `{"error":"Model not supported by provider"}`, "unsupported model"},
		{"server error", http.StatusInternalServerError, `{"trace":"abc"}`, "provider server error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SafeProviderErrorReason(tc.status, []byte(tc.body)); got != tc.want {
				t.Fatalf("reason = %q, want %q", got, tc.want)
			}
		})
	}
}
