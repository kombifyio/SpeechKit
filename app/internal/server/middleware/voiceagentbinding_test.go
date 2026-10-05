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

// A signed owner-instance handle may reach only its exact canonical path.
func TestAuthOwnedInstanceVoiceBindingPreservesExactSignedHandle(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret")
	for _, change := range []string{"current", "handle", "target", "endpoint", "missing", "expired"} {
		t.Run(change, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/voiceagent/sessions", nil)
			signEdgeHeaders(t, request, "edge-secret")
			id := Identity{UserID: "user-42", OrgID: "org-kombify"}
			target, endpoint, handle := "instance:owned-uuid", "https://api.kombify.io/a2a/instances/owned-uuid", "v1.captured.current-signature"
			expiry := strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10)
			if change == "expired" {
				expiry = "1"
			}
			request.Header.Set(VoiceAgentTargetHeader, target)
			request.Header.Set(VoiceAgentEndpointHeader, endpoint)
			request.Header.Set(VoiceAgentLeaseHeader, "current-voice-lease")
			request.Header.Set(VoiceAgentCredentialExpiresAtHeader, expiry)
			request.Header.Set(VoiceAgentInstanceAuthHeader, handle)
			mac := hmac.New(sha256.New, []byte("edge-secret"))
			_, _ = mac.Write([]byte(strings.Join([]string{id.UserID, id.OrgID, target, endpoint, "current-voice-lease", expiry, handle}, "\n")))
			request.Header.Set(VoiceAgentHMACHeader, hex.EncodeToString(mac.Sum(nil)))
			switch change {
			case "handle":
				request.Header.Set(VoiceAgentInstanceAuthHeader, "v1.neighbor.signature")
			case "target":
				request.Header.Set(VoiceAgentTargetHeader, "instance:neighbor-uuid")
			case "endpoint":
				request.Header.Set(VoiceAgentEndpointHeader, "https://api.kombify.io/a2a/agents/owned-uuid")
			case "missing":
				request.Header.Del(VoiceAgentInstanceAuthHeader)
			}
			served := false
			handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				served = true
				binding := VoiceAgentBindingFromContext(r.Context())
				if binding.InstanceAuth != handle || binding.Endpoint != endpoint || binding.TargetAgentID != target {
					t.Fatal("Signed instance handoff was replaced")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if served != (change == "current") || (change != "current" && response.Code != http.StatusUnauthorized) {
				t.Fatal("Unexpected instance authorization effect", served, response.Code)
			}
		})
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

// Sensitive admission boundary: an altered consent snapshot cannot become a
// retained native grant through otherwise valid edge authentication.
func TestAuthVoiceConsentSnapshotRequiresBoundSignature(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret")
	for _, field := range []string{"valid", "cloud", "recording", "stamp", "owner", "lease"} {
		t.Run(field, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/voiceagent/sessions", nil)
			signEdgeHeaders(t, request, "edge-secret")
			id := Identity{UserID: "user-42", OrgID: "org-kombify"}
			expiry := strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10)
			stamp := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
			target, endpoint, lease := "registered-agent", "https://api.kombify.io/a2a/agents/registered-agent", "bound-lease"
			request.Header.Set(VoiceAgentTargetHeader, target)
			request.Header.Set(VoiceAgentEndpointHeader, endpoint)
			request.Header.Set(VoiceAgentLeaseHeader, lease)
			request.Header.Set(VoiceAgentCredentialExpiresAtHeader, expiry)
			sign := func(payload string) string {
				mac := hmac.New(sha256.New, []byte("edge-secret"))
				_, _ = mac.Write([]byte(payload))
				return hex.EncodeToString(mac.Sum(nil))
			}
			request.Header.Set(VoiceAgentHMACHeader, sign(strings.Join([]string{id.UserID, id.OrgID, target, endpoint, lease, expiry}, "\n")))
			request.Header.Set("X-Edge-Voice-Consent-Cloud-Processing", "1")
			request.Header.Set("X-Edge-Voice-Consent-Voice-Agent-Recording", "1")
			request.Header.Set("X-Edge-Voice-Consent-Voice-Agent-Recording-Updated-At", stamp)
			signedOwner, signedLease := id.UserID, lease
			if field == "owner" {
				signedOwner = "other-owner"
			}
			if field == "lease" {
				signedLease = "other-lease"
			}
			request.Header.Set("X-Edge-Voice-Consent-Hmac", sign(strings.Join([]string{signedOwner, id.OrgID, target, signedLease, expiry, "1", "1", stamp}, "\n")))
			switch field {
			case "cloud":
				request.Header.Set("X-Edge-Voice-Consent-Cloud-Processing", "0")
			case "recording":
				request.Header.Set("X-Edge-Voice-Consent-Voice-Agent-Recording", "0")
			case "stamp":
				request.Header.Set("X-Edge-Voice-Consent-Voice-Agent-Recording-Updated-At", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano))
			}
			served := false
			handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				served = true
				binding := VoiceAgentBindingFromContext(r.Context())
				if !binding.ConsentVerified || !binding.CloudProcessing || !binding.VoiceAgentRecording || binding.VoiceAgentRecordingUpdatedAt != stamp {
					t.Error("trusted snapshot was not retained")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if served != (field == "valid") || (field != "valid" && recorder.Code != http.StatusUnauthorized) {
				t.Fatalf("tampered consent admitted: served=%v status=%d", served, recorder.Code)
			}
		})
	}
}
