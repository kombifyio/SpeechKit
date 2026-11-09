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
	req.Header.Set(VoicePrefHeaderSTTPrimary, "deepgram")
	req.Header.Set(VoicePrefHeaderSTTSecondary, "assemblyai")
	req.Header.Set(VoicePrefHeaderVAProvider, "deepgram")
	req.Header.Set(VoicePrefHeaderVAPersona, "concise-de")
}

func signEdgeHeaders(t *testing.T, req *http.Request, secret string) {
	t.Helper()
	if err := SignEdgeEnvelope(req, EdgeKey{ID: "primary", Secret: secret}, Identity{UserID: "user-42", OrgID: "org-kombify", Plan: "pro"}, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// signVoicePrefHeaders mints the dedicated pref-overlay signature over the
// Gateway contract payload: v1\nsub\norgId\nts\nstt-primary\nstt-secondary\n
// va-provider\nva-persona (absent values sign as "").
func signVoicePrefHeaders(t *testing.T, req *http.Request, secret, sub, org, ts string) {
	t.Helper()
	payload := strings.Join([]string{
		"v1", sub, org, ts,
		req.Header.Get(VoicePrefHeaderSTTPrimary),
		req.Header.Get(VoicePrefHeaderSTTSecondary),
		req.Header.Get(VoicePrefHeaderVAProvider),
		req.Header.Get(VoicePrefHeaderVAPersona),
	}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	req.Header.Set(VoicePrefHeaderTimestamp, ts)
	req.Header.Set(VoicePrefHeaderSignature, "v1="+hex.EncodeToString(mac.Sum(nil)))
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

func TestAuth_EdgeHMACAttachesSignedVoicePrefs(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	want := VoicePrefs{
		STTPrimary:   "deepgram",
		STTSecondary: "assemblyai",
		VAProvider:   "deepgram",
		VAPersona:    "concise-de",
	}
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(prefsProbeHandler(t, want))

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", nil)
	signEdgeHeaders(t, req, "edge-secret-xyz")
	setVoicePrefHeaders(req)
	signVoicePrefHeaders(t, req, "edge-secret-xyz", "user-42", "org-kombify", nowUnixString())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuth_EdgeHMACWithoutPrefHeadersYieldsZeroPrefs(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(prefsProbeHandler(t, VoicePrefs{}))

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", nil)
	signEdgeHeaders(t, req, "edge-secret-xyz")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// Pref headers without the dedicated v1 signature (or with an invalid one)
// degrade to "no preference" — the request itself keeps working, because
// preferences are an overlay, never an authorization input.
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
			name: "unsupported signature version",
			prepare: func(t *testing.T, req *http.Request) {
				signVoicePrefHeaders(t, req, "edge-secret-xyz", "user-42", "org-kombify", nowUnixString())
				req.Header.Set(VoicePrefHeaderSignature,
					"v2="+strings.TrimPrefix(req.Header.Get(VoicePrefHeaderSignature), "v1="))
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
			handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(prefsProbeHandler(t, VoicePrefs{}))

			req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", nil)
			signEdgeHeaders(t, req, "edge-secret-xyz")
			setVoicePrefHeaders(req)
			tc.prepare(t, req)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("request must not fail on an invalid pref overlay; got %d body=%s", rec.Code, rec.Body.String())
			}
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

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
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
			prefs := VoicePrefsFromContext(r.Context())
			if prefs.STTPrimary != "deepgram" || prefs.VAPersona != "concise-de" {
				t.Fatalf("prefs not attached on verified edge path: %+v", prefs)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", nil)
	signEdgeHeaders(t, req, "edge-secret-abc")
	setVoicePrefHeaders(req)
	signVoicePrefHeaders(t, req, "edge-secret-abc", "user-42", "org-kombify", nowUnixString())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// Partial preference sets sign absent fields as "" — a primary-only document
// must verify and attach with the remaining fields empty.
func TestAuth_EdgeHMACAttachesPartialPrefSet(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	want := VoicePrefs{STTPrimary: "assemblyai"}
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(prefsProbeHandler(t, want))

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", nil)
	signEdgeHeaders(t, req, "edge-secret-xyz")
	req.Header.Set(VoicePrefHeaderSTTPrimary, "assemblyai")
	signVoicePrefHeaders(t, req, "edge-secret-xyz", "user-42", "org-kombify", nowUnixString())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func setVoicePrefV2Headers(req *http.Request) {
	req.Header.Set(VoicePrefHeaderSTTModel, "stt.deepgram.nova-3")
	req.Header.Set(VoicePrefHeaderTTSModel, "tts.local.kokoro-82m")
	req.Header.Set(VoicePrefHeaderTTSVoice, "kokoro.en.af-bella")
	req.Header.Set(VoicePrefHeaderVAModel, "realtime.builtin.pipeline")
	req.Header.Set(VoicePrefHeaderVocabulary, "kombify.product")
	req.Header.Set(VoicePrefHeaderScope, "edge:dev_0123456789abcdef")
	req.Header.Set(VoicePrefHeaderVALocale, "de-DE")
}

// signVoicePrefV2Headers mints the v2 signature over the Gateway contract
// payload: the v1 lines, then stt-model, tts-model, tts-voice, va-model,
// vocabulary, scope, va-locale (absent values sign as "").
func signVoicePrefV2Headers(t *testing.T, req *http.Request, secret, sub, org, ts string) {
	t.Helper()
	payload := strings.Join([]string{
		"v2", sub, org, ts,
		req.Header.Get(VoicePrefHeaderSTTPrimary),
		req.Header.Get(VoicePrefHeaderSTTSecondary),
		req.Header.Get(VoicePrefHeaderVAProvider),
		req.Header.Get(VoicePrefHeaderVAPersona),
		req.Header.Get(VoicePrefHeaderSTTModel),
		req.Header.Get(VoicePrefHeaderTTSModel),
		req.Header.Get(VoicePrefHeaderTTSVoice),
		req.Header.Get(VoicePrefHeaderVAModel),
		req.Header.Get(VoicePrefHeaderVocabulary),
		req.Header.Get(VoicePrefHeaderScope),
		req.Header.Get(VoicePrefHeaderVALocale),
	}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	req.Header.Set(VoicePrefHeaderTimestamp, ts)
	req.Header.Set(VoicePrefHeaderV2Signature, "v2="+hex.EncodeToString(mac.Sum(nil)))
}

var wantV1Prefs = VoicePrefs{STTPrimary: "deepgram", STTSecondary: "assemblyai", VAProvider: "deepgram", VAPersona: "concise-de"}

func TestAuth_EdgeHMACAttachesSignedV2VoicePrefs(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	want := wantV1Prefs
	want.STTModel = "stt.deepgram.nova-3"
	want.TTSModel = "tts.local.kokoro-82m"
	want.TTSVoice = "kokoro.en.af-bella"
	want.VAModel = "realtime.builtin.pipeline"
	want.Vocabulary = "kombify.product"
	want.Scope = "edge:dev_0123456789abcdef"
	want.VALocale = "de-DE"
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(prefsProbeHandler(t, want))

	req := httptest.NewRequest(http.MethodPost, "/v1/voice-agent/sessions", nil)
	signEdgeHeaders(t, req, "edge-secret-xyz")
	setVoicePrefHeaders(req)
	setVoicePrefV2Headers(req)
	ts := nowUnixString()
	signVoicePrefHeaders(t, req, "edge-secret-xyz", "user-42", "org-kombify", ts)
	signVoicePrefV2Headers(t, req, "edge-secret-xyz", "user-42", "org-kombify", ts)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// A locale-only account (no v1 value) still reaches SpeechKit under v2.
func TestAuth_EdgeHMACAttachesV2OnlyLocale(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(prefsProbeHandler(t, VoicePrefs{VALocale: "de-DE"}))

	req := httptest.NewRequest(http.MethodPost, "/v1/voice-agent/sessions", nil)
	signEdgeHeaders(t, req, "edge-secret-xyz")
	req.Header.Set(VoicePrefHeaderVALocale, "de-DE")
	ts := nowUnixString()
	signVoicePrefHeaders(t, req, "edge-secret-xyz", "user-42", "org-kombify", ts)
	signVoicePrefV2Headers(t, req, "edge-secret-xyz", "user-42", "org-kombify", ts)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// An absent, tampered, foreign or stale v2 signature drops every v2 field;
// a valid v1 signature on the same request keeps the v1 fields.
func TestAuth_EdgeHMACIgnoresInvalidV2PrefOverlay(t *testing.T) {
	cases := []struct {
		name    string
		want    VoicePrefs
		prepare func(t *testing.T, req *http.Request, ts string)
	}{
		{
			name:    "absent v2 signature",
			want:    wantV1Prefs,
			prepare: func(t *testing.T, req *http.Request, ts string) {},
		},
		{
			name: "tampered v2 value after signing",
			want: wantV1Prefs,
			prepare: func(t *testing.T, req *http.Request, ts string) {
				signVoicePrefV2Headers(t, req, "edge-secret-xyz", "user-42", "org-kombify", ts)
				req.Header.Set(VoicePrefHeaderVALocale, "en-US")
			},
		},
		{
			name: "v2 signature minted with the wrong secret",
			want: wantV1Prefs,
			prepare: func(t *testing.T, req *http.Request, ts string) {
				signVoicePrefV2Headers(t, req, "some-other-secret", "user-42", "org-kombify", ts)
			},
		},
		{
			name: "v2 signature bound to a different caller",
			want: wantV1Prefs,
			prepare: func(t *testing.T, req *http.Request, ts string) {
				signVoicePrefV2Headers(t, req, "edge-secret-xyz", "someone-else", "org-kombify", ts)
			},
		},
		{
			name: "v2 signature presented under the v1 version tag",
			want: wantV1Prefs,
			prepare: func(t *testing.T, req *http.Request, ts string) {
				signVoicePrefV2Headers(t, req, "edge-secret-xyz", "user-42", "org-kombify", ts)
				req.Header.Set(VoicePrefHeaderV2Signature, "v1="+strings.TrimPrefix(req.Header.Get(VoicePrefHeaderV2Signature), "v2="))
			},
		},
		{
			name: "stale timestamp outside the replay window drops both versions",
			want: VoicePrefs{},
			prepare: func(t *testing.T, req *http.Request, _ string) {
				stale := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
				signVoicePrefHeaders(t, req, "edge-secret-xyz", "user-42", "org-kombify", stale)
				signVoicePrefV2Headers(t, req, "edge-secret-xyz", "user-42", "org-kombify", stale)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
			handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(prefsProbeHandler(t, tc.want))

			req := httptest.NewRequest(http.MethodPost, "/v1/voice-agent/sessions", nil)
			signEdgeHeaders(t, req, "edge-secret-xyz")
			setVoicePrefHeaders(req)
			setVoicePrefV2Headers(req)
			ts := nowUnixString()
			signVoicePrefHeaders(t, req, "edge-secret-xyz", "user-42", "org-kombify", ts)
			tc.prepare(t, req, ts)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("request must not fail on an invalid pref overlay; got %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// A signed v2 set on a non-HMAC auth path is ignored like v1.
func TestAuth_NonEdgeSourcesIgnoreV2VoicePrefHeaders(t *testing.T) {
	t.Setenv("TEST_BEARER_TOKEN", "service-token-123")
	t.Setenv("TEST_EDGE_SECRET_2", "edge-secret-abc")
	handler := Auth(AuthOptions{Mode: "bearer_or_edge", BearerTokenEnv: "TEST_BEARER_TOKEN", EdgeSecretEnv: "TEST_EDGE_SECRET_2"})(prefsProbeHandler(t, VoicePrefs{}))

	req := httptest.NewRequest(http.MethodPost, "/v1/voice-agent/sessions", nil)
	req.Header.Set("Authorization", "Bearer service-token-123")
	setVoicePrefV2Headers(req)
	ts := nowUnixString()
	signVoicePrefV2Headers(t, req, "edge-secret-abc", "service", "default", ts)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}
