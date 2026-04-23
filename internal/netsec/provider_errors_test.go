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
