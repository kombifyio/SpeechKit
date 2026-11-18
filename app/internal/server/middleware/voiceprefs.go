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
// authenticates (the Gateway's account voice settings document
// `speechkit.voice_prefs.v3`, workspace platform/standards/speechkit-settings.v1.md).
// The edge resolves the caller's stored settings at proxy time and forwards
// them as plain headers; the values are catalogue model ids, persona, voice,
// vocabulary and language NAMES only — never keys or credentials. When the
// caller has no stored settings the edge sends no pref headers at all and the
// server's own configuration stays authoritative.
//
// Trust model (mirrors the Gateway resolver, speechkit-prefs.ts): the
// identity envelope (auth_edge_envelope.go) signs only the identity fields,
// so the preference headers carry their OWN versioned HMAC over the same
// shared edge secret, bound to the verified caller identity and one
// timestamp. One signature travels (x-speechkit-pref-v2-signature: v2=<hex>)
// over 15 lines joined with "\n":
//
//	v2 \n sub \n orgId \n ts \n "" \n "" \n "" \n va-persona \n stt-model \n
//	tts-model \n tts-voice \n va-model \n vocabulary \n scope \n language
//
// Lines 5 to 7 held the retired v1 provider headers and always sign as "";
// `language` is x-speechkit-pref-language (`speech.language`, absent for
// `auto`). Absent values sign as "". The signature uses `x-speechkit-pref-ts`
// (Unix seconds, same replay window as the identity HMAC). The headers are
// honoured ONLY when (a) the request's identity was established via edge-HMAC
// AND (b) the v2 signature verifies against that identity. Preferences are an
// overlay, not an authorization input: a missing or invalid signature
// degrades to "no preference" (debug log) — it never fails the request.
const (
	VoicePrefHeaderVAPersona   = "x-speechkit-pref-va-persona"
	VoicePrefHeaderTimestamp   = "x-speechkit-pref-ts"
	VoicePrefHeaderSTTModel    = "x-speechkit-pref-stt-model"
	VoicePrefHeaderTTSModel    = "x-speechkit-pref-tts-model"
	VoicePrefHeaderTTSVoice    = "x-speechkit-pref-tts-voice"
	VoicePrefHeaderVAModel     = "x-speechkit-pref-va-model"
	VoicePrefHeaderVocabulary  = "x-speechkit-pref-vocabulary"
	VoicePrefHeaderScope       = "x-speechkit-pref-scope"
	VoicePrefHeaderLanguage    = "x-speechkit-pref-language"
	VoicePrefHeaderV2Signature = "x-speechkit-pref-v2-signature"
)

// voicePrefV2SignatureVersion is the accepted pref-signature scheme version
// (the `v2=` prefix on its signature header and the first payload line).
const voicePrefV2SignatureVersion = "v2"

// voicePrefRetiredLines are the payload lines of the retired v1 provider
// headers (stt-primary, stt-secondary, va-provider); they sign as "".
const voicePrefRetiredLines = 3

// VoicePrefs carries the edge-resolved account voice settings for one
// request. All fields are optional; empty means "no preference" and leaves
// the server's own defaults in charge. Preferences are best-effort hints:
// mode handlers fall back to their configured routing instead of failing a
// request whose preference cannot be satisfied.
type VoicePrefs struct {
	// VAPersona names the persona applied when a Voice Agent start frame
	// omits persona_id (`assistant.persona`).
	VAPersona string
	// Model ids are catalogue profile ids (pkg/speechkit/catalog), resolved
	// by the edge along device > category > global.
	STTModel   string // `dictation.model`, e.g. "stt.deepgram.nova-3"
	TTSModel   string // `speech.model`, e.g. "tts.local.kokoro-82m"
	TTSVoice   string // `speech.voice`, the voice name inside TTSModel
	VAModel    string // `assistant.model`, e.g. "realtime.builtin.pipeline"
	Vocabulary string // `vocabulary.set` ref, e.g. "kombify.product"; "none" = off, "" = default
	Scope      string // "<category>:<device>" the edge resolved; "" = unscoped
	// Language is the account's `speech.language` (BCP-47; absent for
	// `auto`), the Voice Agent locale when a start frame names none.
	Language string
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
// header is present or the v2 signature does not verify against the caller's
// identity within the replay window.
func verifiedVoicePrefsFromRequest(r *http.Request, id Identity, secret string, now time.Time) VoicePrefs {
	// Raw values participate in the signature base exactly as injected;
	// trimming happens only after verification.
	values := []string{
		r.Header.Get(VoicePrefHeaderVAPersona),
		r.Header.Get(VoicePrefHeaderSTTModel),
		r.Header.Get(VoicePrefHeaderTTSModel),
		r.Header.Get(VoicePrefHeaderTTSVoice),
		r.Header.Get(VoicePrefHeaderVAModel),
		r.Header.Get(VoicePrefHeaderVocabulary),
		r.Header.Get(VoicePrefHeaderScope),
		r.Header.Get(VoicePrefHeaderLanguage),
	}
	if !anyNonEmpty(values) {
		return VoicePrefs{}
	}
	if secret == "" {
		slog.Debug("voice prefs: ignoring pref headers, no edge secret configured")
		return VoicePrefs{}
	}
	ts := strings.TrimSpace(r.Header.Get(VoicePrefHeaderTimestamp))
	if ts == "" || !edgeTimestampFresh(ts, now) {
		slog.Debug("voice prefs: ignoring pref headers, missing or stale timestamp")
		return VoicePrefs{}
	}
	// Signature base per the Gateway contract (DEPLOYMENT_CONTRACT.md
	// §"SpeechKit User Voice Preferences" / buildSpeechKitPrefV2SignaturePayload):
	// bound to the VERIFIED identity, so a captured pref set cannot be
	// replayed onto another caller.
	lines := append(make([]string, voicePrefRetiredLines), values...)
	if !voicePrefSignatureValid(r.Header.Get(VoicePrefHeaderV2Signature), voicePrefV2SignatureVersion, id, ts, secret, lines) {
		return VoicePrefs{}
	}
	return VoicePrefs{
		VAPersona:  strings.TrimSpace(values[0]),
		STTModel:   strings.TrimSpace(values[1]),
		TTSModel:   strings.TrimSpace(values[2]),
		TTSVoice:   strings.TrimSpace(values[3]),
		VAModel:    strings.TrimSpace(values[4]),
		Vocabulary: strings.TrimSpace(values[5]),
		Scope:      strings.TrimSpace(values[6]),
		Language:   strings.TrimSpace(values[7]),
	}
}

// voicePrefSignatureValid checks one `<version>=<hex>` signature header over
// version \n sub \n orgId \n ts \n values...
func voicePrefSignatureValid(header, version string, id Identity, ts, secret string, values []string) bool {
	presentedVersion, presented, ok := strings.Cut(strings.TrimSpace(header), "=")
	if !ok || presentedVersion != version || presented == "" {
		slog.Debug("voice prefs: ignoring pref headers, missing or unsupported signature version", "version", version)
		return false
	}
	payload := strings.Join(append([]string{version, id.UserID, id.OrgID, ts}, values...), "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmacEqual([]byte(presented), []byte(want)) {
		slog.Debug("voice prefs: ignoring pref headers, signature mismatch", "version", version)
		return false
	}
	return true
}

func anyNonEmpty(values []string) bool {
	for _, value := range values {
		if value != "" {
			return true
		}
	}
	return false
}
