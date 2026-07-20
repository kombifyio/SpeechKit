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
	req.Header.Set(VoicePrefHeaderVAProvider, "gemini")
	req.Header.Set(VoicePrefHeaderVAPersona, "concise-de")
}

func signEdgeHeaders(t *testing.T, req *http.Request, secret string) {
	t.Helper()
	req.Header.Set("X-Edge-User-Id", "user-42")
	req.Header.Set("X-Edge-Org-Id", "org-kombify")
	req.Header.Set("X-Edge-Plan", "pro")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("user-42\norg-kombify\npro\n"))
	req.Header.Set("X-Edge-Auth-Hmac", hex.EncodeToString(mac.Sum(nil)))
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
		VAProvider:   "gemini",
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
