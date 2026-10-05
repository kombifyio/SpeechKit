//go:build linux

package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/responses"
)

const (
	VoiceAgentTargetHeader              = "X-Edge-Voice-Agent-Target"
	VoiceAgentEndpointHeader            = "X-Edge-Voice-Agent-Endpoint"
	VoiceAgentLeaseHeader               = "X-Edge-Voice-Agent-Lease"
	VoiceAgentHMACHeader                = "X-Edge-Voice-Agent-Hmac"
	VoiceAgentCredentialExpiresAtHeader = "X-Edge-Voice-Agent-Credential-Expires-At"
	VoiceAgentInstanceAuthHeader        = "X-Edge-Voice-Agent-Instance-Auth"
)

var voiceAgentTargetPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
var voiceInstanceTargetPattern = regexp.MustCompile(`^instance:([A-Za-z0-9._:-]{1,128})$`)

// VoiceAgentBinding is the authorization decision the Gateway made for one
// registered-agent session. Lease is secret and must never be logged or
// serialized to a client.
type VoiceAgentBinding struct {
	TargetAgentID                string
	Endpoint                     string
	Lease                        string
	CredentialExpiresAt          int64
	InstanceAuth                 string
	ConsentVerified              bool
	CloudProcessing              bool
	VoiceAgentRecording          bool
	VoiceAgentRecordingUpdatedAt string
	EndpointBinding              string
	EndpointSignature            string
	RequestDigest                string
	DirectEndpoint               *responses.Context
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
	if r.Header.Get("X-Edge-Voice-Endpoint-Binding") != "" || r.Header.Get("X-Edge-Voice-Endpoint-Hmac") != "" {
		return verifiedEndpointVoiceBinding(r, id, secret)
	}
	target := strings.TrimSpace(r.Header.Get(VoiceAgentTargetHeader))
	endpoint := strings.TrimSpace(r.Header.Get(VoiceAgentEndpointHeader))
	lease := strings.TrimSpace(r.Header.Get(VoiceAgentLeaseHeader))
	presented := strings.TrimSpace(r.Header.Get(VoiceAgentHMACHeader))
	expiry := strings.TrimSpace(r.Header.Get(VoiceAgentCredentialExpiresAtHeader))
	instanceAuth := strings.TrimSpace(r.Header.Get(VoiceAgentInstanceAuthHeader))
	present := target != "" || endpoint != "" || lease != "" || presented != "" || expiry != "" || instanceAuth != ""
	if !present {
		return VoiceAgentBinding{}, false, nil
	}
	if target == "" || endpoint == "" || lease == "" || presented == "" || strings.TrimSpace(secret) == "" {
		return VoiceAgentBinding{}, true, errors.New("incomplete voice agent binding")
	}
	instance := voiceInstanceTargetPattern.FindStringSubmatch(target)
	if !voiceAgentTargetPattern.MatchString(target) && instance == nil {
		return VoiceAgentBinding{}, true, errors.New("invalid voice agent target")
	}
	parsed, err := url.Parse(endpoint)
	expectedPath := "/a2a/agents/" + target
	if instance != nil {
		expectedPath = "/a2a/instances/" + instance[1]
		if instanceAuth == "" || len(instanceAuth) > 16384 || strings.ContainsAny(instanceAuth, "\r\n") || expiry == "" {
			return VoiceAgentBinding{}, true, errors.New("incomplete voice instance binding")
		}
	} else if instanceAuth != "" {
		return VoiceAgentBinding{}, true, errors.New("unexpected voice instance binding")
	}
	if err != nil || parsed.Scheme != "https" || parsed.Host != "api.kombify.io" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != expectedPath {
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
	if instance != nil {
		payload += "\n" + instanceAuth
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
	binding := VoiceAgentBinding{TargetAgentID: target, Endpoint: endpoint, Lease: lease, CredentialExpiresAt: credentialExpiresAt, InstanceAuth: instanceAuth}
	if err := verifyVoiceConsentSnapshot(r, id, &binding, secret); err != nil {
		return VoiceAgentBinding{}, true, err
	}
	return binding, true, nil
}

func verifiedEndpointVoiceBinding(r *http.Request, id Identity, secret string) (VoiceAgentBinding, bool, error) {
	encoded := r.Header.Get("X-Edge-Voice-Endpoint-Binding")
	signature := r.Header.Get("X-Edge-Voice-Endpoint-Hmac")
	deny := func() (VoiceAgentBinding, bool, error) {
		return VoiceAgentBinding{}, true, errors.New("invalid own endpoint voice binding")
	}
	if secret == "" || encoded == "" || len(encoded) > 8192 || signature == "" || r.Header.Get(VoiceAgentTargetHeader) != "" || r.Header.Get(VoiceAgentLeaseHeader) != "" {
		return deny()
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("speechkit-direct-endpoint-v1\n" + encoded))
	if !hmacEqual([]byte(signature), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		return deny()
	}
	body, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return deny()
	}
	var proof struct {
		Version        int                  `json:"version"`
		Kind           string               `json:"kind"`
		Subject        string               `json:"subject"`
		OrgID          string               `json:"orgId"`
		Connection     responses.Connection `json:"connection"`
		Surface        string               `json:"surface"`
		ConversationID string               `json:"conversationId"`
		RequestDigest  string               `json:"requestDigest"`
		Nonce          string               `json:"nonce"`
		ExpiresAt      int64                `json:"expiresAt"`
		Endpoint       string               `json:"endpoint"`
		Workload       string               `json:"workload"`
		Consent        struct {
			CloudProcessing bool   `json:"cloud_processing"`
			Recording       bool   `json:"voice_agent_recording"`
			Stamp           string `json:"voice_agent_recording_updated_at"`
		} `json:"consent"`
	}
	if json.Unmarshal(body, &proof) != nil || proof.Version != 1 || proof.Kind != "direct-endpoint" || proof.Subject != id.UserID || proof.OrgID != id.OrgID ||
		proof.ExpiresAt <= time.Now().Unix() || proof.Connection.Mode != "endpoint" || proof.Connection.EndpointID == "" || proof.Connection.Model == "" ||
		(proof.Surface != "workbench-direct-chat" && proof.Surface != "floating-panel-direct-chat") || proof.ConversationID == "" || proof.Nonce == "" || len(proof.RequestDigest) != 64 ||
		!proof.Consent.CloudProcessing {
		return deny()
	}
	endpoint, err := url.Parse(proof.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "/v1/ai/responses" ||
		!regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`).MatchString(proof.Workload) {
		return deny()
	}
	if proof.Consent.Recording {
		stamp, err := time.Parse(time.RFC3339Nano, proof.Consent.Stamp)
		if err != nil || stamp.After(time.Now()) {
			return deny()
		}
	}
	return VoiceAgentBinding{Endpoint: proof.Endpoint, CredentialExpiresAt: proof.ExpiresAt,
		ConsentVerified: true, CloudProcessing: true, VoiceAgentRecording: proof.Consent.Recording, VoiceAgentRecordingUpdatedAt: proof.Consent.Stamp,
		EndpointBinding: encoded, EndpointSignature: signature, RequestDigest: proof.RequestDigest,
		DirectEndpoint: &responses.Context{Connection: proof.Connection, Surface: proof.Surface, ConversationID: proof.ConversationID}}, true, nil
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
