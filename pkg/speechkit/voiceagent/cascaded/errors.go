package cascaded

// turnError preserves the machine-readable reason without exposing upstream
// diagnostics through the public live session's error callback.
type turnError struct{ code string }

func (e turnError) Error() string { return FailureMessage(e.code) }
func (e turnError) Code() string  { return e.code }

// IsTerminalFailure identifies outcomes that require a new authorized session
// or a new processor. Server and embedded adapters share this classification.
func IsTerminalFailure(code string) bool {
	switch SafeFailureCode(code) {
	case "auth_expired", "auth_required", "permission_denied", "capability_lease_denied", "internal_panic":
		return true
	default:
		return false
	}
}

// SafeFailureCode restricts untrusted provider reasons to the public outcomes
// understood by voice clients. A syntactically valid identifier can still
// contain credentials or transcript fragments, so syntax alone is insufficient.
func SafeFailureCode(code string) string {
	switch code {
	case "quota_exhausted", "rate_limited", "auth_required", "auth_expired",
		"capability_lease_denied", "permission_denied", "provider_unavailable",
		"provider_failed", "turn_timeout", "stt_failed", "tts_failed", "internal_panic":
		return code
	default:
		return "turn_failed"
	}
}

// FailureMessage returns safe public guidance, never upstream diagnostic text.
func FailureMessage(code string) string {
	switch SafeFailureCode(code) {
	case "quota_exhausted":
		return "The AI credit budget is exhausted."
	case "rate_limited":
		return "The agent is rate limited."
	case "auth_required", "auth_expired":
		return "Start a new authorized voice session."
	case "capability_lease_denied", "permission_denied":
		return "The agent turn is not authorized."
	case "provider_unavailable":
		return "The agent provider is unavailable."
	case "turn_timeout":
		return "The agent turn timed out."
	case "stt_failed":
		return "Speech recognition failed."
	case "tts_failed":
		return "Speech synthesis failed."
	default:
		return "The agent turn failed."
	}
}
