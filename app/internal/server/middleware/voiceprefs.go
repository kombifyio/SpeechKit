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
// authenticates (contracts `speechkit.voice_prefs.v1` and `.v2`). The edge
// resolves the caller's stored voice preferences at proxy time and forwards
// them as plain headers; the values are provider, model, persona, voice,
// vocabulary and locale NAMES only — never keys or credentials. When the
// caller has no stored preferences the edge sends no pref headers at all and
// the server's own configuration stays authoritative.
//
// Trust model (mirrors the Gateway resolver, speechkit-prefs.ts): the
// identity envelope (auth_edge_envelope.go) signs only the identity fields,
// so the preference headers carry their OWN versioned HMACs over the same
// shared edge secret, bound to the verified caller identity and one
// timestamp. Two signatures travel side by side:
//
//	v1 (x-speechkit-pref-signature: v1=<hex>):
//	v1 \n sub \n orgId \n ts \n stt-primary \n stt-secondary \n va-provider \n va-persona
//
//	v2 (x-speechkit-pref-v2-signature: v2=<hex>): the v1 lines, then
//	stt-model \n tts-model \n tts-voice \n va-model \n vocabulary \n scope \n va-locale
//
// (absent values sign as ""; the first line is the version). Both use
// `x-speechkit-pref-ts` (Unix seconds, same replay window as the identity
// HMAC). The headers are honoured ONLY when (a) the request's identity was
// established via edge-HMAC AND (b) the signature of their version verifies
// against that identity: v1 fields need v1, v2 fields need v2. Preferences
// are an overlay, not an authorization input: a missing or invalid signature
// degrades to "no preference" for that version (debug log) — it never fails
// the request.
const (
	VoicePrefHeaderSTTPrimary   = "x-speechkit-pref-stt-primary"
	VoicePrefHeaderSTTSecondary = "x-speechkit-pref-stt-secondary"
	VoicePrefHeaderVAProvider   = "x-speechkit-pref-va-provider"
	VoicePrefHeaderVAPersona    = "x-speechkit-pref-va-persona"
	VoicePrefHeaderTimestamp    = "x-speechkit-pref-ts"
	VoicePrefHeaderSignature    = "x-speechkit-pref-signature"

	VoicePrefHeaderSTTModel    = "x-speechkit-pref-stt-model"
	VoicePrefHeaderTTSModel    = "x-speechkit-pref-tts-model"
	VoicePrefHeaderTTSVoice    = "x-speechkit-pref-tts-voice"
	VoicePrefHeaderVAModel     = "x-speechkit-pref-va-model"
	VoicePrefHeaderVocabulary  = "x-speechkit-pref-vocabulary"
	VoicePrefHeaderScope       = "x-speechkit-pref-scope"
	VoicePrefHeaderVALocale    = "x-speechkit-pref-va-locale"
	VoicePrefHeaderV2Signature = "x-speechkit-pref-v2-signature"
)

// voicePrefSignatureVersion and voicePrefV2SignatureVersion are the accepted
// pref-signature scheme versions (the `vN=` prefix on their signature header
// and the first payload line).
const (
	voicePrefSignatureVersion   = "v1"
	voicePrefV2SignatureVersion = "v2"
)

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

	// v2 (central voice settings). Model ids are catalog profile ids
	// (pkg/speechkit/catalog), resolved by the edge along device > category >
	// global > account. A v2 model takes precedence over the matching v1
	// provider (the Gateway's "a scoped model replaces the account's v1
	// provider").
	STTModel   string // e.g. "stt.deepgram.nova-3"; ahead of STTPrimary
	TTSModel   string // e.g. "tts.local.kokoro-82m"
	TTSVoice   string // voice name inside TTSModel
	VAModel    string // e.g. "realtime.builtin.pipeline"; ahead of VAProvider
	Vocabulary string // transcription vocabulary set ref, e.g. "kombify.product"; "none" = off, "" = default
	Scope      string // "<category>:<device>" the edge resolved; "" = unscoped
	// VALocale is the account's voice_agent.locale (BCP-47), the Voice Agent
	// locale when a start frame names none.
	VALocale string
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
// header is present or no versioned signature verifies against the caller's
// identity within the replay window. The v1 and v2 sets are verified
// independently: an invalid v2 signature drops only the v2 fields.
func verifiedVoicePrefsFromRequest(r *http.Request, id Identity, secret string, now time.Time) VoicePrefs {
	// Raw values participate in the signature base exactly as injected;
	// trimming happens only after verification.
	v1Lines := []string{
		r.Header.Get(VoicePrefHeaderSTTPrimary),
		r.Header.Get(VoicePrefHeaderSTTSecondary),
		r.Header.Get(VoicePrefHeaderVAProvider),
		r.Header.Get(VoicePrefHeaderVAPersona),
	}
	v2Lines := []string{
		r.Header.Get(VoicePrefHeaderSTTModel),
		r.Header.Get(VoicePrefHeaderTTSModel),
		r.Header.Get(VoicePrefHeaderTTSVoice),
		r.Header.Get(VoicePrefHeaderVAModel),
		r.Header.Get(VoicePrefHeaderVocabulary),
		r.Header.Get(VoicePrefHeaderScope),
		r.Header.Get(VoicePrefHeaderVALocale),
	}
	hasV1, hasV2 := anyNonEmpty(v1Lines), anyNonEmpty(v2Lines)
	if !hasV1 && !hasV2 {
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
	var prefs VoicePrefs
	// Signature bases per the Gateway contract (DEPLOYMENT_CONTRACT.md
	// §"SpeechKit User Voice Preferences" / buildSpeechKitPrefSignaturePayload
	// and buildSpeechKitPrefV2SignaturePayload): bound to the VERIFIED
	// identity, so a captured pref set cannot be replayed onto another caller.
	if hasV1 && voicePrefSignatureValid(r.Header.Get(VoicePrefHeaderSignature), voicePrefSignatureVersion, id, ts, secret, v1Lines) {
		prefs.STTPrimary = strings.TrimSpace(v1Lines[0])
		prefs.STTSecondary = strings.TrimSpace(v1Lines[1])
		prefs.VAProvider = strings.TrimSpace(v1Lines[2])
		prefs.VAPersona = strings.TrimSpace(v1Lines[3])
	}
	if hasV2 && voicePrefSignatureValid(r.Header.Get(VoicePrefHeaderV2Signature), voicePrefV2SignatureVersion, id, ts, secret, append(append([]string(nil), v1Lines...), v2Lines...)) {
		prefs.STTModel = strings.TrimSpace(v2Lines[0])
		prefs.TTSModel = strings.TrimSpace(v2Lines[1])
		prefs.TTSVoice = strings.TrimSpace(v2Lines[2])
		prefs.VAModel = strings.TrimSpace(v2Lines[3])
		prefs.Vocabulary = strings.TrimSpace(v2Lines[4])
		prefs.Scope = strings.TrimSpace(v2Lines[5])
		prefs.VALocale = strings.TrimSpace(v2Lines[6])
	}
	return prefs
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
