//go:build linux

package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// edgeSig computes the edge HMAC over the base fields, optionally binding a
// timestamp as the fifth field (mirroring verifyEdgeHMAC's signing order).
func edgeSig(secret, userID, orgID, plan, role, ts string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(userID + "\n" + orgID + "\n" + plan + "\n" + role))
	if ts != "" {
		mac.Write([]byte("\n" + ts))
	}
	return hex.EncodeToString(mac.Sum(nil))
}

func edgeRequest(userID, orgID, plan, role, ts, sig string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	req.Header.Set("X-Edge-User-Id", userID)
	req.Header.Set("X-Edge-Org-Id", orgID)
	req.Header.Set("X-Edge-Plan", plan)
	if role != "" {
		req.Header.Set("X-Edge-Role", role)
	}
	if ts != "" {
		req.Header.Set("X-Edge-Auth-Ts", ts)
	}
	req.Header.Set("X-Edge-Auth-Hmac", sig)
	return req
}

func TestAuth_EdgeHMAC_TimestampBound_Valid(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := edgeSig("edge-secret-xyz", "user-42", "org-kombify", "pro", "admin", ts)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, edgeRequest("user-42", "org-kombify", "pro", "admin", ts, sig))

	if rec.Code != http.StatusOK {
		t.Fatalf("fresh ts-bound signature should pass; got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuth_EdgeHMAC_TimestampStale_Rejected(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)
	// 10 minutes old — outside the 5-minute window. Signature is otherwise valid.
	ts := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	sig := edgeSig("edge-secret-xyz", "user-42", "org-kombify", "pro", "admin", ts)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, edgeRequest("user-42", "org-kombify", "pro", "admin", ts, sig))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stale ts-bound signature must be rejected; got %d", rec.Code)
	}
}

func TestAuth_EdgeHMAC_TimestampMalformed_Rejected(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)
	ts := "not-a-number"
	sig := edgeSig("edge-secret-xyz", "user-42", "org-kombify", "pro", "admin", ts)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, edgeRequest("user-42", "org-kombify", "pro", "admin", ts, sig))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("malformed ts must be rejected; got %d", rec.Code)
	}
}

// TestAuth_EdgeHMAC_DowngradeStripTimestamp_Rejected proves that stripping the
// X-Edge-Auth-Ts header from a captured ts-bound request cannot downgrade it to
// the legacy (replayable) path: the presented HMAC was computed over the
// ts-bound base and no longer matches the legacy base.
func TestAuth_EdgeHMAC_DowngradeStripTimestamp_Rejected(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := edgeSig("edge-secret-xyz", "user-42", "org-kombify", "pro", "admin", ts)

	// Replay WITHOUT the ts header but with the ts-bound signature.
	req := edgeRequest("user-42", "org-kombify", "pro", "admin", "", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("downgrade (stripped ts) must be rejected; got %d", rec.Code)
	}
}

// TestAuth_EdgeHMAC_LegacyNoTimestamp_StillAccepted guards the backward-compat
// path: an edge signer that does not send X-Edge-Auth-Ts keeps working.
func TestAuth_EdgeHMAC_LegacyNoTimestamp_StillAccepted(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)
	sig := edgeSig("edge-secret-xyz", "user-42", "org-kombify", "pro", "admin", "")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, edgeRequest("user-42", "org-kombify", "pro", "admin", "", sig))

	if rec.Code != http.StatusOK {
		t.Fatalf("legacy no-ts signature must still pass; got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestEdgeTimestampFresh(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name string
		ts   string
		want bool
	}{
		{"exact now", "1700000000", true},
		{"within window past", strconv.FormatInt(now.Add(-4*time.Minute).Unix(), 10), true},
		{"within window future", strconv.FormatInt(now.Add(4*time.Minute).Unix(), 10), true},
		{"too old", strconv.FormatInt(now.Add(-6*time.Minute).Unix(), 10), false},
		{"too far future", strconv.FormatInt(now.Add(6*time.Minute).Unix(), 10), false},
		{"malformed", "abc", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := edgeTimestampFresh(tt.ts, now); got != tt.want {
				t.Fatalf("edgeTimestampFresh(%q) = %v, want %v", tt.ts, got, tt.want)
			}
		})
	}
}
