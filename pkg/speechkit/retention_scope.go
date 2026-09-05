package speechkit

// retention_scope.go is the second privacy policy axis, orthogonal to
// NetworkScope. NetworkScope answers "where may this process reach?";
// RetentionScope answers "what may outlive the work that produced it?".
//
// They are deliberately separate. device_only says nothing about local
// persistence — an install with no network at all still writes every
// transcript, meeting review and screen capture to disk and keeps them
// forever. Conversely a zero-retention posture is meaningful with open network
// access. Folding retention into NetworkScope would force a false choice
// between reaching a provider and keeping a recording.
//
// Like NetworkScope, this is enforced at the backend boundary — the store for
// persistence, provider assembly for the vendor side — and the UI only mirrors
// the decision through stable disabled-reason IDs.

import (
	"errors"
	"fmt"
	"strings"
)

// RetentionScope selects how long user content survives.
type RetentionScope string

const (
	// RetentionScopeRetain is the default and the historical behaviour:
	// content is durable and disappears only through the configured retention
	// settings, a deletion, or a subject erasure request.
	RetentionScopeRetain RetentionScope = "retain"

	// RetentionScopeEphemeral keeps nothing beyond the work that produced it.
	//
	// Content is still written while that work is in flight — a meeting's
	// transcript has to be readable for the rolling digest engine, and a
	// process that dies mid-meeting must not take the recording with it — and
	// is swept when the meeting or session ends. What survives is the
	// generated result (a Meeting Review), not the raw material behind it.
	//
	// On the vendor side this scope also refuses any provider surface that
	// cannot assert no-store, because a promise that stops at the device would
	// be misleading.
	RetentionScopeEphemeral RetentionScope = "ephemeral"
)

// ErrUnknownRetentionScope is returned for values outside the known set.
// Config loading fails closed on it instead of guessing.
var ErrUnknownRetentionScope = errors.New("speechkit: unknown retention scope")

// Stable, localizable disabled-reason IDs, mirroring the NetworkScopeReason*
// set. The backend attaches these; UIs render the matching catalog message
// rather than inventing copy.
const (
	// RetentionScopeReasonEphemeral marks an artifact that is not kept because
	// the retention scope is ephemeral.
	RetentionScopeReasonEphemeral = "sk.privacy.disabled.retention_ephemeral"
	// RetentionScopeReasonProviderRetains marks a provider that cannot be told
	// to drop the request content.
	RetentionScopeReasonProviderRetains = "sk.privacy.disabled.provider_retains"
)

// ArtifactClass groups durable artifacts by what they hold, so a retention
// policy can distinguish a recording from a generated result and from the
// user's own settings.
type ArtifactClass string

const (
	// ArtifactClassRecording is raw captured material and anything mechanically
	// derived from it: transcripts, meeting segments, rolling digests, voice
	// agent turns, stored audio.
	ArtifactClassRecording ArtifactClass = "recording"

	// ArtifactClassScreenCapture is a picture of the user's screen taken
	// during a meeting. Split from ArtifactClassRecording because it is the
	// most sensitive artifact and may warrant its own rule later.
	ArtifactClassScreenCapture ArtifactClass = "screen_capture"

	// ArtifactClassNote is what the user typed themselves during a meeting.
	ArtifactClassNote ArtifactClass = "note"

	// ArtifactClassReview is a generated result the user is meant to keep and
	// read afterwards: a Meeting Review and its executive brief.
	ArtifactClassReview ArtifactClass = "review"

	// ArtifactClassSettings is user-authored configuration — Words,
	// Replacements, templates. It is not a record of anything that was said,
	// so no retention scope drops it.
	ArtifactClassSettings ArtifactClass = "settings"
)

// ParseRetentionScope maps a raw config/API value onto a RetentionScope. The
// empty string is the backwards-compatible default (retain); unknown values
// return ErrUnknownRetentionScope so callers fail closed rather than guessing
// which way a typo was meant.
func ParseRetentionScope(raw string) (RetentionScope, error) {
	switch RetentionScope(strings.ToLower(strings.TrimSpace(raw))) {
	case "", RetentionScopeRetain:
		return RetentionScopeRetain, nil
	case RetentionScopeEphemeral:
		return RetentionScopeEphemeral, nil
	default:
		return "", fmt.Errorf("%w: %q (expected retain or ephemeral)", ErrUnknownRetentionScope, raw)
	}
}

// NormalizeRetentionScope is the runtime-safe variant of ParseRetentionScope:
// an unparseable value collapses to the strictest scope, so a mutation that
// bypassed config validation can never quietly start keeping recordings.
//
// The parse path already rejects unknown values on load and on save, so this
// fallback is defence in depth rather than the normal route.
func NormalizeRetentionScope(raw string) RetentionScope {
	scope, err := ParseRetentionScope(raw)
	if err != nil {
		return RetentionScopeEphemeral
	}
	return scope
}

// Valid reports whether s is one of the known scopes.
func (s RetentionScope) Valid() bool {
	switch s {
	case RetentionScopeRetain, RetentionScopeEphemeral:
		return true
	default:
		return false
	}
}

// Restricted reports whether s sweeps content rather than keeping it.
func (s RetentionScope) Restricted() bool {
	return s != RetentionScopeRetain
}

// Durable reports whether artifacts of this class outlive the work that
// produced them, with the stable disabled-reason ID when they do not.
//
// A false answer does not mean "never write it": under the ephemeral scope a
// meeting's transcript is written so the digest engine can read it and a crash
// cannot lose it, and is swept when the meeting ends. It means "do not keep
// it", and the sweep is what enforces that.
func (s RetentionScope) Durable(class ArtifactClass) (bool, string) {
	if s == RetentionScopeRetain {
		return true, ""
	}
	// ephemeral — and any unknown value, which fails closed to it.
	switch class {
	case ArtifactClassSettings:
		// Words and Replacements are configuration the user typed on purpose,
		// not a record of what they said. Dropping them would erase settings
		// under the banner of privacy.
		return true, ""
	case ArtifactClassReview:
		// The generated write-up is the point of having recorded at all, and
		// it holds the least raw material. It stays; the transcript, notes,
		// digests and screen captures behind it do not.
		return true, ""
	default:
		return false, RetentionScopeReasonEphemeral
	}
}

// ProviderRetention is what a provider surface can promise about keeping the
// content of a request. It mirrors the no-store position each provider
// declares in its option manifest, expressed here as a small value type so the
// root package keeps holding contracts only.
type ProviderRetention string

const (
	// ProviderRetentionNoStore means the surface does not keep request content
	// beyond answering — either because SpeechKit sends a per-request flag, or
	// because the surface does not retain by default (a runtime on the user's
	// own machine being the trivial case).
	ProviderRetentionNoStore ProviderRetention = "no_store"

	// ProviderRetentionUnknown means the surface cannot assert no-store from a
	// request: the posture is an account or project setting, it is unverified,
	// or the vendor offers a flag SpeechKit does not send on that surface.
	// Treated as "retains", because an unverifiable promise is not one.
	ProviderRetentionUnknown ProviderRetention = "unknown"
)

// AllowsProviderRetention reports whether a provider surface with the given
// retention promise may be used under this scope, with the disabled reason ID
// when it may not.
//
// The strict answer is deliberate. Under the ephemeral scope the device keeps
// nothing, so a provider that keeps the audio for thirty days would make the
// setting read as a promise it does not deliver — worse than offering no
// setting at all.
func (s RetentionScope) AllowsProviderRetention(r ProviderRetention) (bool, string) {
	if s == RetentionScopeRetain {
		return true, ""
	}
	if r == ProviderRetentionNoStore {
		return true, ""
	}
	return false, RetentionScopeReasonProviderRetains
}
