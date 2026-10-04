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
	"strconv"
	"strings"
	"time"
)

const (
	VoiceAgentTargetHeader              = "X-Edge-Voice-Agent-Target"
	VoiceAgentEndpointHeader            = "X-Edge-Voice-Agent-Endpoint"
	VoiceAgentLeaseHeader               = "X-Edge-Voice-Agent-Lease"
	VoiceAgentHMACHeader                = "X-Edge-Voice-Agent-Hmac"
	VoiceAgentCredentialExpiresAtHeader = "X-Edge-Voice-Agent-Credential-Expires-At"
)

var voiceAgentTargetPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

// VoiceAgentBinding is the authorization decision the Gateway made for one
// registered-agent session. Lease is secret and must never be logged or
// serialized to a client.
type VoiceAgentBinding struct {
	TargetAgentID                string
	Endpoint                     string
	Lease                        string
	CredentialExpiresAt          int64
	ConsentVerified              bool
	CloudProcessing              bool
	VoiceAgentRecording          bool
	VoiceAgentRecordingUpdatedAt string
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
	expiry := strings.TrimSpace(r.Header.Get(VoiceAgentCredentialExpiresAtHeader))
	present := target != "" || endpoint != "" || lease != "" || presented != "" || expiry != ""
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
	var credentialExpiresAt int64
	if expiry != "" {
		credentialExpiresAt, err = strconv.ParseInt(expiry, 10, 64)
		if err != nil || credentialExpiresAt <= 0 || strconv.FormatInt(credentialExpiresAt, 10) != expiry {
			return VoiceAgentBinding{}, true, errors.New("invalid voice agent credential expiry")
		}
		// Optional sixth field preserves legacy bindings. This value can only
		// narrow the signed lease lifetime; it never authorizes renewal.
		payload += "\n" + expiry
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmacEqual([]byte(presented), []byte(want)) {
		return VoiceAgentBinding{}, true, errors.New("invalid voice agent binding signature")
	}
	if credentialExpiresAt > 0 && credentialExpiresAt <= time.Now().Unix() {
		return VoiceAgentBinding{}, true, errors.New("voice agent credential expired")
	}
	binding := VoiceAgentBinding{TargetAgentID: target, Endpoint: endpoint, Lease: lease, CredentialExpiresAt: credentialExpiresAt}
	if err := verifyVoiceConsentSnapshot(r, id, &binding, secret); err != nil {
		return VoiceAgentBinding{}, true, err
	}
	return binding, true, nil
}

func verifyVoiceConsentSnapshot(r *http.Request, id Identity, binding *VoiceAgentBinding, secret string) error {
	cloud := r.Header.Get("X-Edge-Voice-Consent-Cloud-Processing")
	recording := r.Header.Get("X-Edge-Voice-Consent-Voice-Agent-Recording")
	stamp := r.Header.Get("X-Edge-Voice-Consent-Voice-Agent-Recording-Updated-At")
	presented := r.Header.Get("X-Edge-Voice-Consent-Hmac")
	if cloud == "" && recording == "" && stamp == "" && presented == "" {
		return nil // Legacy registered pipeline remains compatible; native denies absence.
	}
	if (cloud != "0" && cloud != "1") || (recording != "0" && recording != "1") || binding.CredentialExpiresAt <= 0 || presented == "" {
		return errors.New("incomplete voice consent decision")
	}
	if recording == "1" {
		when, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil || when.After(time.Now()) {
			return errors.New("invalid voice recording consent stamp")
		}
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(strings.Join([]string{id.UserID, id.OrgID, binding.TargetAgentID, binding.Lease, strconv.FormatInt(binding.CredentialExpiresAt, 10), cloud, recording, stamp}, "\n")))
	if !hmacEqual([]byte(presented), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		return errors.New("invalid voice consent signature")
	}
	binding.ConsentVerified, binding.CloudProcessing = true, cloud == "1"
	binding.VoiceAgentRecording, binding.VoiceAgentRecordingUpdatedAt = recording == "1", stamp
	return nil
}
