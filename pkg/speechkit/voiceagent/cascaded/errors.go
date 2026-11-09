package cascaded

// turnError preserves the machine-readable reason without exposing upstream
// diagnostics through the public live session's error callback.
type turnError struct{ code string }

func (e turnError) Error() string { return FailureMessage(e.code) }
func (e turnError) Code() string  { return e.code }

// ErrorClass is the one user-facing reason class of a voice failure. Every
// voice surface (Box, Android, Companion, web Composer) renders its text from
// the class, so a used-up limit never reads like a missing right and a
// provider outage never reads like a denial (AI-HARNESS-CONCEPT-STANDARD §14
// rule 4: denials name their cause).
type ErrorClass string

const (
	// ClassLimit: a usage limit of the account or device is reached (credit
	// budget, rate limit, conversations per device). Waiting or ending another
	// conversation helps; the plan may raise it.
	ClassLimit ErrorClass = "limit"
	// ClassOutage: the service or a provider could not answer. Nothing on the
	// user's side is wrong; trying again later helps.
	ClassOutage ErrorClass = "outage"
	// ClassRight: the account, plan or agent lacks the right for this turn.
	ClassRight ErrorClass = "right"
	// ClassConsent: cloud voice or recording consent is missing.
	ClassConsent ErrorClass = "consent"
	// ClassExpired: the session's authorization ran out; start a new session.
	ClassExpired ErrorClass = "expired"
)

// FailureClass maps a public voice wire code to its class. Only codes the
// table names carry a class; any other code (a credential rejection, a request
// or protocol fault, an unknown reason) carries none, and clients apply their
// own code rules to it. A credential rejection (unauthenticated,
// invalid_ticket, a plain 401) is never "expired": the Box re-pairs and an app
// signs in again, which its code rules decide.
func FailureClass(code string) ErrorClass {
	switch code {
	case "quota_exhausted", "rate_limited", "voice_budget_exhausted",
		"voice_budget_unavailable", "voice_budget_required",
		"per_device_limit_exceeded", "account_device_limit_exceeded":
		return ClassLimit
	case "auth_expired", "authorization_expired", "ticket_expired":
		return ClassExpired
	case "capability_lease_denied", "permission_denied":
		return ClassRight
	case "voice_consent_required":
		return ClassConsent
	case "provider_unavailable", "provider_failed", "provider_receive_failed", "provider_connect_failed",
		"turn_failed", "turn_timeout", "stt_failed", "tts_failed", "internal_panic", "global_limit_exceeded":
		return ClassOutage
	default:
		// Deliberately unclassed, among others: unauthenticated, invalid_ticket,
		// auth_required (credential rejections); audio_download_disabled (a
		// deployment setting); origin_not_allowed, voice_session_owner_mismatch,
		// voice_agent_binding_mismatch (binding faults); invalid_frame,
		// conversation_control_busy, audio_transport_mismatch,
		// tool_response_unsupported (request and protocol faults).
		return ""
	}
}

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
		return "Too many requests right now. Wait a moment, then try again."
	case "auth_required", "auth_expired":
		return "Start a new authorized voice session."
	case "capability_lease_denied", "permission_denied":
		return "This account does not have the right for this agent turn."
	case "provider_unavailable":
		return "The agent provider is unavailable. Try again in a moment."
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
