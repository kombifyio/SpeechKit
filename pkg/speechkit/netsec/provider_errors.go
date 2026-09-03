package netsec

import (
	"fmt"
	"net/http"
	"strings"
)

// ProviderStatusError returns a user-safe upstream provider error. It keeps
// the provider name and HTTP status, but never includes the raw response body.
func ProviderStatusError(provider string, status int, body []byte) error {
	return fmt.Errorf("%s error (%d): %s", provider, status, SafeProviderErrorReason(status, body))
}

// SafeProviderErrorReason classifies a provider response body without exposing
// body contents to UI, logs, or higher-level error messages.
func SafeProviderErrorReason(status int, body []byte) string {
	lower := strings.ToLower(string(body))
	switch {
	case mentionsContextLimit(lower):
		// Checked before the authentication case: these bodies talk about
		// "tokens" ("request (9016 tokens) exceeds the available context size
		// (4096 tokens)" from llama-server, "maximum context length is 8192
		// tokens" from OpenAI-compatible servers) and used to read as an
		// authentication failure, which nothing retries or splits.
		return "provider context limit exceeded"
	case status == http.StatusUnauthorized || status == http.StatusForbidden ||
		strings.Contains(lower, "api key") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "forbidden") ||
		strings.Contains(lower, "credential") ||
		mentionsAuthToken(lower):
		return "provider authentication failed"
	case status == http.StatusTooManyRequests ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "rate_limit"):
		return "provider rate limit exceeded"
	case strings.Contains(lower, "insufficient_quota") ||
		strings.Contains(lower, "quota") ||
		strings.Contains(lower, "billing"):
		return "provider quota exhausted"
	case strings.Contains(lower, "model not supported") ||
		strings.Contains(lower, "unsupported model"):
		return "unsupported model"
	case strings.Contains(lower, "model not found"):
		return "model not found"
	case status == http.StatusServiceUnavailable ||
		strings.Contains(lower, "model loading") ||
		strings.Contains(lower, "loading"):
		return "provider temporarily unavailable"
	case status >= 500:
		return "provider server error"
	case status >= 400:
		return "provider rejected request"
	default:
		return "provider request failed"
	}
}

// mentionsContextLimit recognises the "input too long for the model" family
// of provider answers.
func mentionsContextLimit(lower string) bool {
	for _, needle := range []string{
		"context size",
		"context length",
		"context window",
		"exceed_context_size",
		"exceeds the available context",
		"maximum context",
		"too many tokens",
		"prompt is too long",
		"input is too long",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// mentionsAuthToken matches the word "token" only in its credential sense.
// A bare "token" substring also matched "max_tokens" and token counts in
// context-size errors.
func mentionsAuthToken(lower string) bool {
	for _, needle := range []string{
		"invalid token",
		"invalid_token",
		"token expired",
		"expired token",
		"bearer token",
		"access token",
		"auth token",
		"api token",
		"authentication token",
		"missing token",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
