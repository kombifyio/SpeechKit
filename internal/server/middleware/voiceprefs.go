//go:build linux

package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Voice-preference headers a fronting edge proxy injects on requests it
// authenticates (contract `speechkit.voice_prefs.v1`). The edge resolves the
// caller's stored voice preferences at proxy time and forwards them as plain
// headers; the values are provider and persona NAMES only — never keys or
// credentials. When the caller has no stored preferences the edge sends no
// pref headers at all and the server's own configuration stays authoritative.
//
// Trust model (mirrors the Gateway resolver, speechkit-prefs.ts): the
// existing identity HMAC (X-Edge-Auth-Hmac) signs only the identity fields,
// so the preference headers carry their OWN versioned HMAC over the same
// shared edge secret, bound to the verified caller identity and a timestamp:
//
//	v1 \n sub \n orgId \n ts \n stt-primary \n stt-secondary \n va-provider \n va-persona
//
// (absent values sign as ""). The signature arrives as
// `x-speechkit-pref-signature: v1=<hex>` plus `x-speechkit-pref-ts` (Unix
// seconds, same replay window as the identity HMAC). The headers are
// honoured ONLY when (a) the request's identity was established via
// edge-HMAC AND (b) this signature verifies against that identity.
// Preferences are an overlay, not an authorization input: a missing or
// invalid signature degrades to "no preference" (debug log) — it never fails
// the request.
const (
	VoicePrefHeaderSTTPrimary   = "x-speechkit-pref-stt-primary"
	VoicePrefHeaderSTTSecondary = "x-speechkit-pref-stt-secondary"
	VoicePrefHeaderVAProvider   = "x-speechkit-pref-va-provider"
	VoicePrefHeaderVAPersona    = "x-speechkit-pref-va-persona"
	VoicePrefHeaderTimestamp    = "x-speechkit-pref-ts"
	VoicePrefHeaderSignature    = "x-speechkit-pref-signature"
)

// voicePrefSignatureVersion is the accepted pref-signature scheme version
// (the `v1=` prefix on x-speechkit-pref-signature and the first payload
// field).
const voicePrefSignatureVersion = "v1"

// VoicePrefs carries the edge-resolved user voice preferences for one
// request. All fields are optional; empty means "no preference" and leaves
// the server's own defaults in charge. Preferences are best-effort hints:
// mode handlers fall back to their configured routing instead of failing a
// request whose preference cannot be satisfied.
type VoicePrefs struct {
	// STTPrimary and STTSecondary name the preferred Dictation STT providers
	// (e.g. "deepgram", "assemblyai"), tried in that order.
	STTPrimary   string
	STTSecondary string
	// VAProvider names the preferred Voice Agent realtime backend used when a
	// session's start frame omits an explicit provider.
	VAProvider string
	// VAPersona names the persona applied when a Voice Agent start frame
	// omits persona_id.
	VAPersona string
}

// IsZero reports whether no preference was forwarded.
func (p VoicePrefs) IsZero() bool { return p == (VoicePrefs{}) }

type voicePrefsCtxKey struct{}

// VoicePrefsFromContext returns the voice preferences attached by Auth, or
// the zero value when the request did not authenticate via edge-HMAC, the
// edge forwarded no preference headers, or the preference signature did not
// verify.
func VoicePrefsFromContext(ctx context.Context) VoicePrefs {
	if v, ok := ctx.Value(voicePrefsCtxKey{}).(VoicePrefs); ok {
		return v
	}
	return VoicePrefs{}
}

// InjectVoicePrefsForTest attaches VoicePrefs to the context using the same
// unexported key the Auth middleware uses. Exported solely so handler tests
// in external packages can exercise preference resolution without spinning up
// the full auth middleware (mirror of InjectIdentityForTest). Production code
// MUST NOT use this.
func InjectVoicePrefsForTest(ctx context.Context, prefs VoicePrefs) context.Context {
	return context.WithValue(ctx, voicePrefsCtxKey{}, prefs)
}

// verifiedVoicePrefsFromRequest validates the edge preference overlay for a
// request whose identity was already proven via edge-HMAC. Returns the zero
// value (never an error — prefs are an overlay, not authz) when no pref
// header is present or the versioned signature does not verify against the
// caller's identity within the replay window.
func verifiedVoicePrefsFromRequest(r *http.Request, id Identity, secret string, now time.Time) VoicePrefs {
	// Raw values participate in the signature base exactly as injected;
	// trimming happens only after verification.
	rawPrimary := r.Header.Get(VoicePrefHeaderSTTPrimary)
	rawSecondary := r.Header.Get(VoicePrefHeaderSTTSecondary)
	rawVAProvider := r.Header.Get(VoicePrefHeaderVAProvider)
	rawVAPersona := r.Header.Get(VoicePrefHeaderVAPersona)
	if rawPrimary == "" && rawSecondary == "" && rawVAProvider == "" && rawVAPersona == "" {
		return VoicePrefs{}
	}
	if secret == "" {
		slog.Debug("voice prefs: ignoring pref headers, no edge secret configured")
		return VoicePrefs{}
	}
	version, presented, ok := strings.Cut(strings.TrimSpace(r.Header.Get(VoicePrefHeaderSignature)), "=")
	if !ok || version != voicePrefSignatureVersion || presented == "" {
		slog.Debug("voice prefs: ignoring pref headers, missing or unsupported signature version")
		return VoicePrefs{}
	}
	ts := strings.TrimSpace(r.Header.Get(VoicePrefHeaderTimestamp))
	if ts == "" || !edgeTimestampFresh(ts, now) {
		slog.Debug("voice prefs: ignoring pref headers, missing or stale timestamp")
		return VoicePrefs{}
	}
	// Signature base per the Gateway contract (DEPLOYMENT_CONTRACT.md
	// §"SpeechKit User Voice Preferences" / buildSpeechKitPrefSignaturePayload):
	// bound to the VERIFIED identity, so a captured pref set cannot be
	// replayed onto another caller.
	payload := strings.Join([]string{
		voicePrefSignatureVersion,
		id.UserID,
		id.OrgID,
		ts,
		rawPrimary,
		rawSecondary,
		rawVAProvider,
		rawVAPersona,
	}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmacEqual([]byte(presented), []byte(want)) {
		slog.Debug("voice prefs: ignoring pref headers, signature mismatch")
		return VoicePrefs{}
	}
	return VoicePrefs{
		STTPrimary:   strings.TrimSpace(rawPrimary),
		STTSecondary: strings.TrimSpace(rawSecondary),
		VAProvider:   strings.TrimSpace(rawVAProvider),
		VAPersona:    strings.TrimSpace(rawVAPersona),
	}
}
