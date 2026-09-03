//go:build linux

package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	VoiceAgentTargetHeader   = "X-Edge-Voice-Agent-Target"
	VoiceAgentEndpointHeader = "X-Edge-Voice-Agent-Endpoint"
	VoiceAgentLeaseHeader    = "X-Edge-Voice-Agent-Lease"
	VoiceAgentHMACHeader     = "X-Edge-Voice-Agent-Hmac"
)

var voiceAgentTargetPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

// VoiceAgentBinding is the authorization decision the Gateway made for one
// registered-agent session. Lease is secret and must never be logged or
// serialized to a client.
type VoiceAgentBinding struct {
	TargetAgentID string
	Endpoint      string
	Lease         string
}

type voiceAgentBindingCtxKey struct{}

func VoiceAgentBindingFromContext(ctx context.Context) VoiceAgentBinding {
	if binding, ok := ctx.Value(voiceAgentBindingCtxKey{}).(VoiceAgentBinding); ok {
		return binding
	}
	return VoiceAgentBinding{}
}

func InjectVoiceAgentBindingForTest(ctx context.Context, binding VoiceAgentBinding) context.Context {
	return context.WithValue(ctx, voiceAgentBindingCtxKey{}, binding)
}

func verifiedVoiceAgentBindingFromRequest(r *http.Request, id Identity, secret string) (VoiceAgentBinding, bool, error) {
	target := strings.TrimSpace(r.Header.Get(VoiceAgentTargetHeader))
	endpoint := strings.TrimSpace(r.Header.Get(VoiceAgentEndpointHeader))
	lease := strings.TrimSpace(r.Header.Get(VoiceAgentLeaseHeader))
	presented := strings.TrimSpace(r.Header.Get(VoiceAgentHMACHeader))
	present := target != "" || endpoint != "" || lease != "" || presented != ""
	if !present {
		return VoiceAgentBinding{}, false, nil
	}
	if target == "" || endpoint == "" || lease == "" || presented == "" || strings.TrimSpace(secret) == "" {
		return VoiceAgentBinding{}, true, errors.New("incomplete voice agent binding")
	}
	if !voiceAgentTargetPattern.MatchString(target) {
		return VoiceAgentBinding{}, true, errors.New("invalid voice agent target")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "api.kombify.io" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "/a2a/agents/"+target {
		return VoiceAgentBinding{}, true, errors.New("invalid voice agent endpoint")
	}
	payload := strings.Join([]string{id.UserID, id.OrgID, target, endpoint, lease}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmacEqual([]byte(presented), []byte(want)) {
		return VoiceAgentBinding{}, true, errors.New("invalid voice agent binding signature")
	}
	return VoiceAgentBinding{TargetAgentID: target, Endpoint: endpoint, Lease: lease}, true, nil
}
