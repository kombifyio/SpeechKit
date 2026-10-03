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

func TestVerifiedVoiceAgentBindingRequiresCompleteValidEdgeDecision(t *testing.T) {
	id := Identity{UserID: "auth0|owner", OrgID: "org_test"}
	endpoint := "https://api.kombify.io/a2a/agents/kombify-ai"
	request := httptest.NewRequest("POST", "/v1/voiceagent/sessions", nil)
	request.Header.Set(VoiceAgentTargetHeader, "kombify-ai")
	request.Header.Set(VoiceAgentEndpointHeader, endpoint)
	request.Header.Set(VoiceAgentLeaseHeader, "lease-token")
	request.Header.Set(VoiceAgentHMACHeader, bindingHMAC("edge-secret", id, "kombify-ai", endpoint, "lease-token"))

	binding, present, err := verifiedVoiceAgentBindingFromRequest(request, id, "edge-secret")
	if err != nil || !present || binding.TargetAgentID != "kombify-ai" || binding.Lease != "lease-token" {
		t.Fatalf("valid binding rejected: binding=%+v present=%v err=%v", binding, present, err)
	}

	request.Header.Set(VoiceAgentTargetHeader, "other-agent")
	if _, _, err := verifiedVoiceAgentBindingFromRequest(request, id, "edge-secret"); err == nil {
		t.Fatal("tampered target must fail closed")
	}
}

func TestAuthVoiceAgentCredentialExpiryIsAuthenticated(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret")
	validExpiry := strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10)
	for _, tc := range []struct {
		name              string
		signed, presented string
		allowed           bool
	}{
		{name: "legacy", allowed: true},
		{name: "issuer expiry", signed: validExpiry, presented: validExpiry, allowed: true},
		{name: "tampered", signed: validExpiry, presented: validExpiry + "0"},
		{name: "unsigned", presented: validExpiry},
		{name: "removed", signed: validExpiry},
		{name: "expired", signed: "1", presented: "1"},
		{name: "noncanonical", signed: "0" + validExpiry, presented: "0" + validExpiry},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/voiceagent/sessions", nil)
			signEdgeHeaders(t, request, "edge-secret")
			id := Identity{UserID: "user-42", OrgID: "org-kombify"}
			endpoint := "https://api.kombify.io/a2a/agents/kombify-ai"
			request.Header.Set(VoiceAgentTargetHeader, "kombify-ai")
			request.Header.Set(VoiceAgentEndpointHeader, endpoint)
			request.Header.Set(VoiceAgentLeaseHeader, "lease-token")
			payload := id.UserID + "\n" + id.OrgID + "\nkombify-ai\n" + endpoint + "\nlease-token"
			if tc.signed != "" {
				payload += "\n" + tc.signed
			}
			mac := hmac.New(sha256.New, []byte("edge-secret"))
			_, _ = mac.Write([]byte(payload))
			request.Header.Set(VoiceAgentHMACHeader, hex.EncodeToString(mac.Sum(nil)))
			if tc.presented != "" {
				request.Header.Set(VoiceAgentCredentialExpiresAtHeader, tc.presented)
			}
			served := false
			handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				served = true
				binding := VoiceAgentBindingFromContext(r.Context())
				if tc.presented != "" && strconv.FormatInt(binding.CredentialExpiresAt, 10) != tc.presented {
					t.Fatal("issuer expiry was not attached to the authenticated session binding")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if served != tc.allowed || (!tc.allowed && recorder.Code != http.StatusUnauthorized) {
				t.Fatalf("request authorization: served=%v status=%d", served, recorder.Code)
			}
		})
	}
}

func bindingHMAC(secret string, id Identity, target, endpoint, lease string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(id.UserID + "\n" + id.OrgID + "\n" + target + "\n" + endpoint + "\n" + lease))
	return hex.EncodeToString(mac.Sum(nil))
}
