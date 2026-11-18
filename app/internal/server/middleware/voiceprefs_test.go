//go:build linux

package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func setVoicePrefHeaders(req *http.Request) {
	req.Header.Set(VoicePrefHeaderVAPersona, "concise-de")
	req.Header.Set(VoicePrefHeaderSTTModel, "stt.deepgram.nova-3")
	req.Header.Set(VoicePrefHeaderTTSModel, "tts.local.kokoro-82m")
	req.Header.Set(VoicePrefHeaderTTSVoice, "kokoro.en.af-bella")
	req.Header.Set(VoicePrefHeaderVAModel, "realtime.builtin.pipeline")
	req.Header.Set(VoicePrefHeaderVocabulary, "kombify.product")
	req.Header.Set(VoicePrefHeaderScope, "edge:dev_0123456789abcdef")
	req.Header.Set(VoicePrefHeaderLanguage, "de-DE")
}

var wantPrefs = VoicePrefs{
	VAPersona:  "concise-de",
	STTModel:   "stt.deepgram.nova-3",
	TTSModel:   "tts.local.kokoro-82m",
	TTSVoice:   "kokoro.en.af-bella",
	VAModel:    "realtime.builtin.pipeline",
	Vocabulary: "kombify.product",
	Scope:      "edge:dev_0123456789abcdef",
	Language:   "de-DE",
}

func signEdgeHeaders(t *testing.T, req *http.Request, secret string) {
	t.Helper()
	if err := SignEdgeEnvelope(req, EdgeKey{ID: "primary", Secret: secret}, Identity{UserID: "user-42", OrgID: "org-kombify", Plan: "pro"}, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// signVoicePrefHeaders mints the v2 signature over the Gateway contract
// payload (buildSpeechKitPrefV2SignaturePayload): v2, sub, orgId, ts, three
// empty lines where the retired v1 provider headers stood, then va-persona,
// stt-model, tts-model, tts-voice, va-model, vocabulary, scope, language
// (absent values sign as "").
func signVoicePrefHeaders(t *testing.T, req *http.Request, secret, sub, org, ts string) {
	t.Helper()
	payload := strings.Join([]string{
		"v2", sub, org, ts,
		"", "", "",
		req.Header.Get(VoicePrefHeaderVAPersona),
		req.Header.Get(VoicePrefHeaderSTTModel),
		req.Header.Get(VoicePrefHeaderTTSModel),
		req.Header.Get(VoicePrefHeaderTTSVoice),
		req.Header.Get(VoicePrefHeaderVAModel),
		req.Header.Get(VoicePrefHeaderVocabulary),
		req.Header.Get(VoicePrefHeaderScope),
		req.Header.Get(VoicePrefHeaderLanguage),
	}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	req.Header.Set(VoicePrefHeaderTimestamp, ts)
	req.Header.Set(VoicePrefHeaderV2Signature, "v2="+hex.EncodeToString(mac.Sum(nil)))
}

func nowUnixString() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}

func prefsProbeHandler(t *testing.T, want VoicePrefs) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if prefs := VoicePrefsFromContext(r.Context()); prefs != want {
			t.Fatalf("prefs = %+v, want %+v", prefs, want)
		}
		w.WriteHeader(http.StatusOK)
	})
}

func serveOK(t *testing.T, handler http.Handler, req *http.Request) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("request must not fail on the pref overlay; got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuth_EdgeHMACAttachesSignedVoicePrefs(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(prefsProbeHandler(t, wantPrefs))

	req := httptest.NewRequest(http.MethodPost, "/v1/voice-agent/sessions", nil)
	signEdgeHeaders(t, req, "edge-secret-xyz")
	setVoicePrefHeaders(req)
	signVoicePrefHeaders(t, req, "edge-secret-xyz", "user-42", "org-kombify", nowUnixString())
	serveOK(t, handler, req)
}

func TestAuth_EdgeHMACWithoutPrefHeadersYieldsZeroPrefs(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(prefsProbeHandler(t, VoicePrefs{}))

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", nil)
	signEdgeHeaders(t, req, "edge-secret-xyz")
	serveOK(t, handler, req)
}

// Partial preference sets sign absent fields as "": a language-only account
// verifies and attaches with the remaining fields empty.
func TestAuth_EdgeHMACAttachesPartialPrefSet(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(prefsProbeHandler(t, VoicePrefs{Language: "de-DE"}))

	req := httptest.NewRequest(http.MethodPost, "/v1/voice-agent/sessions", nil)
	signEdgeHeaders(t, req, "edge-secret-xyz")
	req.Header.Set(VoicePrefHeaderLanguage, "de-DE")
	signVoicePrefHeaders(t, req, "edge-secret-xyz", "user-42", "org-kombify", nowUnixString())
	serveOK(t, handler, req)
}

// The retired v1 set (x-speechkit-pref-stt-primary, -stt-secondary,
// -va-provider, -va-locale and the v1 signature) carries no preference any
// more: a request holding only those, even correctly v1-signed, attaches
// nothing, and a v2 signature that signs non-empty retired lines does not
// verify.
func TestAuth_EdgeHMACIgnoresRetiredV1PrefSet(t *testing.T) {
	const secret = "edge-secret-xyz"
	retired := map[string]string{
		"x-speechkit-pref-stt-primary":   "deepgram",
		"x-speechkit-pref-stt-secondary": "assemblyai",
		"x-speechkit-pref-va-provider":   "deepgram",
		"x-speechkit-pref-va-locale":     "de-DE",
	}
	t.Run("v1 headers and v1 signature only", func(t *testing.T) {
		t.Setenv("TEST_EDGE_SECRET", secret)
		handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(prefsProbeHandler(t, VoicePrefs{}))
		req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", nil)
		signEdgeHeaders(t, req, secret)
		for name, value := range retired {
			req.Header.Set(name, value)
		}
		ts := nowUnixString()
		payload := strings.Join([]string{"v1", "user-42", "org-kombify", ts, "deepgram", "assemblyai", "deepgram", ""}, "\n")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(payload))
		req.Header.Set(VoicePrefHeaderTimestamp, ts)
		req.Header.Set("x-speechkit-pref-signature", "v1="+hex.EncodeToString(mac.Sum(nil)))
		serveOK(t, handler, req)
	})
	t.Run("v2 signature over non-empty retired lines", func(t *testing.T) {
		t.Setenv("TEST_EDGE_SECRET", secret)
		handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(prefsProbeHandler(t, VoicePrefs{}))
		req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", nil)
		signEdgeHeaders(t, req, secret)
		req.Header.Set(VoicePrefHeaderSTTModel, "stt.deepgram.nova-3")
		ts := nowUnixString()
		payload := strings.Join([]string{"v2", "user-42", "org-kombify", ts, "deepgram", "", "", "", "stt.deepgram.nova-3", "", "", "", "", "", ""}, "\n")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(payload))
		req.Header.Set(VoicePrefHeaderTimestamp, ts)
		req.Header.Set(VoicePrefHeaderV2Signature, "v2="+hex.EncodeToString(mac.Sum(nil)))
		serveOK(t, handler, req)
	})
}

// An absent, tampered, foreign, stale or mis-versioned signature drops the
// whole overlay; the request itself keeps working, because preferences are
// an overlay, never an authorization input.
func TestAuth_EdgeHMACIgnoresUnsignedOrInvalidPrefOverlay(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, req *http.Request)
	}{
		{
			name:    "missing pref signature",
			prepare: func(t *testing.T, req *http.Request) {},
		},
		{
			name: "tampered value after signing",
			prepare: func(t *testing.T, req *http.Request) {
				signVoicePrefHeaders(t, req, "edge-secret-xyz", "user-42", "org-kombify", nowUnixString())
				req.Header.Set(VoicePrefHeaderLanguage, "en-US")
			},
		},
		{
			name: "signature minted with the wrong secret",
			prepare: func(t *testing.T, req *http.Request) {
				signVoicePrefHeaders(t, req, "some-other-secret", "user-42", "org-kombify", nowUnixString())
			},
		},
		{
			name: "signature bound to a different caller",
			prepare: func(t *testing.T, req *http.Request) {
				signVoicePrefHeaders(t, req, "edge-secret-xyz", "someone-else", "org-kombify", nowUnixString())
			},
		},
		{
			name: "stale timestamp outside the replay window",
			prepare: func(t *testing.T, req *http.Request) {
				stale := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
				signVoicePrefHeaders(t, req, "edge-secret-xyz", "user-42", "org-kombify", stale)
			},
		},
		{
			name: "v2 signature presented under the v1 version tag",
			prepare: func(t *testing.T, req *http.Request) {
				signVoicePrefHeaders(t, req, "edge-secret-xyz", "user-42", "org-kombify", nowUnixString())
				req.Header.Set(VoicePrefHeaderV2Signature,
					"v1="+strings.TrimPrefix(req.Header.Get(VoicePrefHeaderV2Signature), "v2="))
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
			handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(prefsProbeHandler(t, VoicePrefs{}))

			req := httptest.NewRequest(http.MethodPost, "/v1/voice-agent/sessions", nil)
			signEdgeHeaders(t, req, "edge-secret-xyz")
			setVoicePrefHeaders(req)
			tc.prepare(t, req)
			serveOK(t, handler, req)
		})
	}
}

// A raw client on a non-HMAC auth path must not be able to smuggle
// preference headers past the edge — even a correctly signed overlay is
// ignored when the identity source is not edge_hmac.
func TestAuth_NonEdgeSourcesIgnoreVoicePrefHeaders(t *testing.T) {
	t.Setenv("TEST_BEARER_TOKEN", "service-token-123")
	t.Setenv("TEST_EDGE_SECRET_2", "edge-secret-abc")

	handler := Auth(AuthOptions{
		Mode:           "bearer_or_edge",
		BearerTokenEnv: "TEST_BEARER_TOKEN",
		EdgeSecretEnv:  "TEST_EDGE_SECRET_2",
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := IdentityFromContext(r.Context())
			if id.Source != "bearer" {
				t.Fatalf("expected bearer identity, got %+v", id)
			}
			if prefs := VoicePrefsFromContext(r.Context()); !prefs.IsZero() {
				t.Fatalf("bearer caller must not inject voice prefs, got %+v", prefs)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", nil)
	req.Header.Set("Authorization", "Bearer service-token-123")
	setVoicePrefHeaders(req)
	signVoicePrefHeaders(t, req, "edge-secret-abc", "service", "default", nowUnixString())
	serveOK(t, handler, req)
}

// bearer_or_edge with a verified edge signature keeps the edge trust
// boundary: the same middleware config that ignored bearer-path headers
// above honours a correctly signed overlay on the edge path.
func TestAuth_BearerOrEdgeHonoursSignedPrefsOnEdgePath(t *testing.T) {
	t.Setenv("TEST_BEARER_TOKEN", "service-token-123")
	t.Setenv("TEST_EDGE_SECRET_2", "edge-secret-abc")

	handler := Auth(AuthOptions{
		Mode:           "bearer_or_edge",
		BearerTokenEnv: "TEST_BEARER_TOKEN",
		EdgeSecretEnv:  "TEST_EDGE_SECRET_2",
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := IdentityFromContext(r.Context())
			if id.Source != "edge_hmac" {
				t.Fatalf("expected edge_hmac identity, got %+v", id)
			}
			if prefs := VoicePrefsFromContext(r.Context()); prefs != wantPrefs {
				t.Fatalf("prefs not attached on verified edge path: %+v", prefs)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", nil)
	signEdgeHeaders(t, req, "edge-secret-abc")
	setVoicePrefHeaders(req)
	signVoicePrefHeaders(t, req, "edge-secret-abc", "user-42", "org-kombify", nowUnixString())
	serveOK(t, handler, req)
}
