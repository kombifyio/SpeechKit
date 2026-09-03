//go:build linux

package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"testing"
)

func TestVerifiedVoiceAgentBindingRequiresCompleteValidEdgeDecision(t *testing.T) {
	id := Identity{UserID: "auth0|owner", OrgID: "org_test"}
	endpoint := "https://api.kombify.io/a2a/agents/mobile-companion"
	request := httptest.NewRequest("POST", "/v1/voiceagent/sessions", nil)
	request.Header.Set(VoiceAgentTargetHeader, "mobile-companion")
	request.Header.Set(VoiceAgentEndpointHeader, endpoint)
	request.Header.Set(VoiceAgentLeaseHeader, "lease-token")
	request.Header.Set(VoiceAgentHMACHeader, bindingHMAC("edge-secret", id, "mobile-companion", endpoint, "lease-token"))

	binding, present, err := verifiedVoiceAgentBindingFromRequest(request, id, "edge-secret")
	if err != nil || !present || binding.TargetAgentID != "mobile-companion" || binding.Lease != "lease-token" {
		t.Fatalf("valid binding rejected: binding=%+v present=%v err=%v", binding, present, err)
	}

	request.Header.Set(VoiceAgentTargetHeader, "other-agent")
	if _, _, err := verifiedVoiceAgentBindingFromRequest(request, id, "edge-secret"); err == nil {
		t.Fatal("tampered target must fail closed")
	}
}

func bindingHMAC(secret string, id Identity, target, endpoint, lease string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(id.UserID + "\n" + id.OrgID + "\n" + target + "\n" + endpoint + "\n" + lease))
	return hex.EncodeToString(mac.Sum(nil))
}
