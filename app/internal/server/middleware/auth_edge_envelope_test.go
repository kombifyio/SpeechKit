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

const envelopeTestSecret = "edge-envelope-secret"

func envelopeAuth(t *testing.T, next http.Handler) http.Handler {
	t.Helper()
	return Auth(AuthOptions{
		Mode:               "edge_hmac",
		EdgeSecretProvider: func() string { return envelopeTestSecret },
		EdgeKeysProvider:   func() []EdgeKey { return []EdgeKey{{ID: "primary", Secret: envelopeTestSecret}} },
	})(next)
}

func signedEnvelopeRequest(t *testing.T, method, target string, key EdgeKey) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, target, nil)
	if err := SignEdgeEnvelope(r, key, Identity{UserID: "user-42", OrgID: "org-1", Plan: "pro", Role: "admin"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	return r
}

func serve(h http.Handler, r *http.Request) int {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code
}

func TestEdgeEnvelopeAcceptedWithVoiceBudgetOverlay(t *testing.T) {
	var got Identity
	var budget VoiceBudget
	h := envelopeAuth(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = IdentityFromContext(r.Context())
		budget = VoiceBudgetFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	r := signedEnvelopeRequest(t, http.MethodPost, "/api/v1/voiceagent/sessions?mode=live", EdgeKey{ID: "primary", Secret: envelopeTestSecret})
	expiry := strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10)
	mac := hmac.New(sha256.New, []byte(envelopeTestSecret))
	_, _ = mac.Write([]byte(strings.Join([]string{"speechkit.voice_budget.v1", "user-42", "org-1", "reservation-1", expiry}, "\n")))
	r.Header.Set(VoiceBudgetReservationHeader, "reservation-1")
	r.Header.Set(VoiceBudgetExpiresAtHeader, expiry)
	r.Header.Set(VoiceBudgetHMACHeader, hex.EncodeToString(mac.Sum(nil)))

	if code := serve(h, r); code != http.StatusOK {
		t.Fatalf("valid envelope rejected: %d", code)
	}
	if got.UserID != "user-42" || got.OrgID != "org-1" || got.Plan != "pro" || got.Role != "admin" {
		t.Fatalf("identity not taken from the envelope: %+v", got)
	}
	if budget.ReservationID != "reservation-1" {
		t.Fatalf("voice budget overlay not applied: %+v", budget)
	}
}

func TestEdgeEnvelopeReplayRejected(t *testing.T) {
	h := envelopeAuth(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	first := signedEnvelopeRequest(t, http.MethodGet, "/api/v1/settings", EdgeKey{ID: "primary", Secret: envelopeTestSecret})
	replay := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	replay.Header = first.Header.Clone()

	if code := serve(h, first); code != http.StatusOK {
		t.Fatalf("first use rejected: %d", code)
	}
	if code := serve(h, replay); code != http.StatusUnauthorized {
		t.Fatalf("replayed envelope must be rejected; got %d", code)
	}
}

func TestEdgeEnvelopeWrongRequestBindingRejected(t *testing.T) {
	h := envelopeAuth(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	signed := signedEnvelopeRequest(t, http.MethodGet, "/api/v1/settings", EdgeKey{ID: "primary", Secret: envelopeTestSecret})
	// Same envelope presented for another path.
	moved := httptest.NewRequest(http.MethodGet, "/api/v1/deployment/status", nil)
	moved.Header = signed.Header.Clone()

	if code := serve(h, moved); code != http.StatusUnauthorized {
		t.Fatalf("envelope bound to /api/v1/settings accepted for another path; got %d", code)
	}
}

func TestEdgeEnvelopeUnknownKeyIDRejected(t *testing.T) {
	h := envelopeAuth(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	r := signedEnvelopeRequest(t, http.MethodGet, "/api/v1/settings", EdgeKey{ID: "retired", Secret: envelopeTestSecret})

	if code := serve(h, r); code != http.StatusUnauthorized {
		t.Fatalf("envelope under an unknown key id accepted; got %d", code)
	}
}

// TestLegacyEdgeHMACRejected: the retired replayable X-Edge-Auth-Hmac
// identity signature no longer authenticates, even when correctly signed.
func TestLegacyEdgeHMACRejected(t *testing.T) {
	h := envelopeAuth(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(envelopeTestSecret))
	mac.Write([]byte(strings.Join([]string{"user-42", "org-1", "pro", "admin", ts}, "\n")))
	r := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	r.Header.Set("X-Edge-User-Id", "user-42")
	r.Header.Set("X-Edge-Org-Id", "org-1")
	r.Header.Set("X-Edge-Plan", "pro")
	r.Header.Set("X-Edge-Role", "admin")
	r.Header.Set("X-Edge-Auth-Ts", ts)
	r.Header.Set("X-Edge-Auth-Hmac", hex.EncodeToString(mac.Sum(nil)))

	if code := serve(h, r); code != http.StatusUnauthorized {
		t.Fatalf("legacy HMAC accepted; got %d", code)
	}
}
