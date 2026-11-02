//go:build linux

package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const envelopeTestSecret = "edge-envelope-secret"

func TestAnonymousEdgeCannotEnterAccountRoutes(t *testing.T) {
	for _, path := range []string{"/api/v1/settings", "/api/v1/voiceagent/sessions", "/api/v1/anonymous/voiceagent/sessions/any/renew", "/api/v1/personas"} {
		t.Run(path, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, path, nil)
			if err := SignEdgeEnvelope(r, EdgeKey{ID: "primary", Secret: envelopeTestSecret}, Identity{UserID: "anonymous:voice:synthetic", OrgID: "default", Role: "anonymous", Plan: "anonymous"}, time.Now()); err != nil {
				t.Fatal(err)
			}
			h := envelopeAuth(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
			if got := serve(h, r); got != http.StatusUnauthorized {
				t.Fatalf("anonymous identity entered private route: %d", got)
			}
		})
	}
}

func TestAnonymousRouteRequiresScopedEdgeIdentity(t *testing.T) {
	for _, id := range []Identity{
		{UserID: "anonymous:voice:synthetic", OrgID: "default", Role: "anonymous", Plan: "anonymous"},
		{UserID: "user-42", OrgID: "org-1", Role: "admin", Plan: "pro"},
		{UserID: "anonymous:voice:synthetic", OrgID: "default", Role: "admin", Plan: "anonymous"},
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/anonymous/voiceagent/sessions", nil)
		if err := SignEdgeEnvelope(r, EdgeKey{ID: "primary", Secret: envelopeTestSecret}, id, time.Now()); err != nil {
			t.Fatal(err)
		}
		h := envelopeAuth(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
		want := http.StatusUnauthorized
		if IsAnonymousEdge(Identity{UserID: id.UserID, OrgID: id.OrgID, Role: id.Role, Plan: id.Plan, Source: "edge_hmac"}) {
			want = http.StatusOK
		}
		if got := serve(h, r); got != want {
			t.Fatalf("anonymous admission got %d want %d", got, want)
		}
	}
}

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

func TestEdgeEnvelopeVoiceBudgetReceiptBinding(t *testing.T) {
	receipt := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	expiry := strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10)
	for _, tc := range []struct {
		name        string
		mutate      func(*http.Request, *Identity)
		signReceipt bool
		want        int
	}{
		{name: "v2 accepted", want: http.StatusOK},
		{name: "tampered receipt", mutate: func(r *http.Request, _ *Identity) {
			r.Header.Set(VoiceBudgetReceiptHeader, "B"+receipt[1:])
		}},
		{name: "stripped receipt", mutate: func(r *http.Request, _ *Identity) { r.Header.Del(VoiceBudgetReceiptHeader) }},
		{name: "duplicate receipt", mutate: func(r *http.Request, _ *Identity) { r.Header.Add(VoiceBudgetReceiptHeader, receipt) }},
		{name: "empty receipt", signReceipt: true, mutate: func(r *http.Request, _ *Identity) { r.Header.Set(VoiceBudgetReceiptHeader, "") }},
		{name: "noncanonical trailing bits", signReceipt: true, mutate: func(r *http.Request, _ *Identity) {
			r.Header.Set(VoiceBudgetReceiptHeader, receipt[:42]+"B")
		}},
		{name: "padded receipt", signReceipt: true, mutate: func(r *http.Request, _ *Identity) { r.Header.Set(VoiceBudgetReceiptHeader, receipt+"=") }},
		{name: "whitespace receipt", signReceipt: true, mutate: func(r *http.Request, _ *Identity) { r.Header.Set(VoiceBudgetReceiptHeader, " "+receipt) }},
		{name: "short receipt", signReceipt: true, mutate: func(r *http.Request, _ *Identity) { r.Header.Set(VoiceBudgetReceiptHeader, receipt[:42]) }},
		{name: "actor binding", mutate: func(_ *http.Request, id *Identity) { id.UserID = "auth0:other-actor" }},
		{name: "org binding", mutate: func(_ *http.Request, id *Identity) { id.OrgID = "other-org" }},
		{name: "reservation binding", mutate: func(r *http.Request, _ *Identity) { r.Header.Set(VoiceBudgetReservationHeader, "other-reservation") }},
		{name: "expiry binding", mutate: func(r *http.Request, _ *Identity) {
			r.Header.Set(VoiceBudgetExpiresAtHeader, strconv.FormatInt(time.Now().Add(2*time.Minute).Unix(), 10))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := Identity{UserID: "auth0:actor", OrgID: "default", Plan: "pro", Role: "admin"}
			r := httptest.NewRequest(http.MethodPost, "/api/v1/voiceagent/sessions", nil)
			mac := hmac.New(sha256.New, []byte(envelopeTestSecret))
			_, _ = mac.Write([]byte(strings.Join([]string{"speechkit.voice_budget.v2", id.UserID, id.OrgID, "reservation-1", expiry, receipt}, "\n")))
			r.Header.Set(VoiceBudgetReservationHeader, "reservation-1")
			r.Header.Set(VoiceBudgetExpiresAtHeader, expiry)
			r.Header.Set(VoiceBudgetReceiptHeader, receipt)
			r.Header.Set(VoiceBudgetHMACHeader, hex.EncodeToString(mac.Sum(nil)))
			if tc.mutate != nil {
				tc.mutate(r, &id)
			}
			if tc.signReceipt {
				// A valid HMAC over malformed receipt text must still fail the
				// canonical capability check, not merely the signature check.
				mac.Reset()
				_, _ = mac.Write([]byte(strings.Join([]string{"speechkit.voice_budget.v2", id.UserID, id.OrgID, "reservation-1", expiry, r.Header.Get(VoiceBudgetReceiptHeader)}, "\n")))
				r.Header.Set(VoiceBudgetHMACHeader, hex.EncodeToString(mac.Sum(nil)))
			}
			// Re-sign the outer envelope so failures discriminate budget binding
			// from the independent caller/request authentication.
			if err := SignEdgeEnvelope(r, EdgeKey{ID: "primary", Secret: envelopeTestSecret}, id, time.Now()); err != nil {
				t.Fatal(err)
			}
			called := false
			h := envelopeAuth(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				budget := VoiceBudgetFromContext(r.Context())
				if budget.ReceiptToken != receipt || budget.ReservationID != "reservation-1" || strconv.FormatInt(budget.ExpiresAt, 10) != expiry {
					t.Error("verified reservation capability was not retained")
				}
				public, err := json.Marshal(budget)
				if err != nil || strings.Contains(string(public), receipt) || strings.Contains(string(public), "ReceiptToken") {
					t.Error("reservation capability escaped into public budget JSON")
				}
				w.WriteHeader(http.StatusOK)
			}))
			want := tc.want
			if want == 0 {
				want = http.StatusUnauthorized
			}
			if code := serve(h, r); code != want {
				t.Fatalf("budget authentication = %d, want %d", code, want)
			}
			if called != (want == http.StatusOK) {
				t.Fatal("denied budget reached the protected handler")
			}
		})
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

// The v3 envelope binds the edge-resolved device; SpeechKit counts sessions per
// device on it (D-2026-10-26-14), so only a signed device may reach Identity.
func TestEdgeEnvelopeV3DeviceBinding(t *testing.T) {
	key := EdgeKey{ID: "primary", Secret: envelopeTestSecret}
	device := "dev_" + strings.Repeat("A", 43)
	for _, tc := range []struct {
		name       string
		device     string
		mutate     func(*http.Request)
		want       int
		wantDevice string
	}{
		{name: "v3 device accepted", device: device, want: http.StatusOK, wantDevice: device},
		{name: "client bucket accepted", device: "client:abc123", want: http.StatusOK, wantDevice: "client:abc123"},
		{name: "v3 device swapped", device: device, mutate: func(r *http.Request) { r.Header.Set(EdgeDeviceIDHeader, "dev_"+strings.Repeat("B", 43)) }, want: http.StatusUnauthorized},
		{name: "v3 device stripped", device: device, mutate: func(r *http.Request) { r.Header.Del(EdgeDeviceIDHeader) }, want: http.StatusUnauthorized},
		{name: "v3 downgraded to v2", device: device, mutate: func(r *http.Request) {
			r.Header.Set(EdgeAuthSignatureHeader, "v2="+strings.TrimPrefix(r.Header.Get(EdgeAuthSignatureHeader), "v3="))
		}, want: http.StatusUnauthorized},
		{name: "unsigned device beside v2 ignored", mutate: func(r *http.Request) { r.Header.Set(EdgeDeviceIDHeader, device) }, want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got Identity
			h := envelopeAuth(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = IdentityFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			}))
			r := httptest.NewRequest(http.MethodPost, "/api/v1/voiceagent/sessions", nil)
			if err := SignEdgeEnvelope(r, key, Identity{UserID: "user-42", OrgID: "org-1", Plan: "pro", DeviceID: tc.device}, time.Now()); err != nil {
				t.Fatal(err)
			}
			if tc.mutate != nil {
				tc.mutate(r)
			}
			if code := serve(h, r); code != tc.want {
				t.Fatalf("status %d, want %d", code, tc.want)
			}
			if tc.want == http.StatusOK && got.DeviceID != tc.wantDevice {
				t.Fatalf("device %q, want %q", got.DeviceID, tc.wantDevice)
			}
		})
	}
	if err := SignEdgeEnvelope(httptest.NewRequest(http.MethodPost, "/x", nil), key, Identity{UserID: "u", OrgID: "o", DeviceID: "dev with space"}, time.Now()); err == nil {
		t.Fatal("a device id with whitespace was signed")
	}
}

// Fixed vector shared with the fronting edge signer, so both implementations
// build the same v3 payload.
func TestEdgeEnvelopeV3PayloadVector(t *testing.T) {
	payload := edgeEnvelopePayload("primary", "post", "/api/v1/voiceagent/sessions", "auth0|user-42", "org-1", "pro", "", "dev_"+strings.Repeat("A", 43), "1767225600", "nonce-1")
	if got := signEdgePayload("speechkit-edge-v3-vector-secret", payload); got != "mGH2JLj00hb7yfKbEJVvJ6V_vMrsRnC6ehrV5LRlMac" {
		t.Fatalf("v3 vector signature %s", got)
	}
}
