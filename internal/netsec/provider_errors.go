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
	case status == http.StatusUnauthorized || status == http.StatusForbidden ||
		strings.Contains(lower, "api key") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "forbidden") ||
		strings.Contains(lower, "credential") ||
		strings.Contains(lower, "token"):
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
