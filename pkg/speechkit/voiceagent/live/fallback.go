package live

import "strings"

// ShouldTryFallback reports whether a configured fallback model is worth
// attempting after the primary failed: it must be set, and it must not be the
// model that just failed.
func ShouldTryFallback(primary, fallback string) bool {
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return false
	}
	return strings.TrimSpace(primary) != fallback
}
