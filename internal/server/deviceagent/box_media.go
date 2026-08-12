package deviceagent

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/deviceagent"
)

const (
	BoxMediaTurnPath = "/v1/box-media/turn"

	BoxMediaDeviceIDHeader       = "X-SpeechKit-Device-ID"
	BoxMediaPairingIDHeader      = "X-SpeechKit-Pairing-ID"
	BoxMediaRequestIDHeader      = "X-SpeechKit-Request-ID"
	BoxMediaInputSHA256Header    = "X-SpeechKit-Input-Audio-SHA256"
	BoxMediaOutputSHA256Header   = "X-SpeechKit-Output-Audio-SHA256"
	BoxMediaReplayHeader         = "X-SpeechKit-Replayed"
	boxMediaContentType          = "audio/L16; rate=48000; channels=1"
	boxMediaInputRate            = 16000
	boxMediaOutputRate           = 48000
	boxMediaChannels             = 1
	boxMediaBytesPerSample       = 2
	boxMediaMinimumDuration      = 300 * time.Millisecond
	boxMediaMaximumDuration      = 15 * time.Second
	boxMediaDefaultSTTTimeout    = 60 * time.Second
	boxMediaDefaultTurnTimeout   = 60 * time.Second
	boxMediaMaximumStageTimeout  = 2 * time.Minute
	boxMediaMaximumResponseBytes = boxMediaOutputRate * boxMediaBytesPerSample * 30
)

var (
	ErrBoxMediaBridgeRequired   = errors.New("box media: device-agent bridge is required")
	ErrBoxMediaLocalSTTRequired = errors.New("box media: a proven local-only STT transcriber is required")
	ErrBoxMediaTokenInvalid     = errors.New("box media: an independent pairing token is required")
	ErrBoxMediaRuleInvalid      = errors.New("box media: the single transcript mapping is invalid")
)

// BoxMediaTranscript is the bounded result returned by the host's local STT
// dependency. Provider identifies the local implementation for evidence only;
// it is never returned to the Box.
type BoxMediaTranscript struct {
	Text     string
	Provider string
}

// BoxMediaLocalTranscriber deliberately differs from the general STT router:
// LocalOnly must attest that this dependency has no cloud or dynamic fallback.
// Production wiring passes the router's concrete Local() provider directly.
type BoxMediaLocalTranscriber interface {
	LocalOnly() bool
	TranscribeLocal(context.Context, []byte, string) (*BoxMediaTranscript, error)
}

// BoxMediaRuleOptions defines exactly one server-owned transcript-to-command
// mapping for the Phase-A representative light. There is no rule collection,
// fuzzy matching, search, intent inference, or fallback.
type BoxMediaRuleOptions struct {
	DeviceID   string
	PairingID  string
	RoomID     string
	Transcript string
	CommandID  string
	Locale     string
}

type BoxMediaHandlerOptions struct {
	Bridge            *Bridge
	LocalSTT          BoxMediaLocalTranscriber
	MediaPairingToken string
	Rule              BoxMediaRuleOptions
	STTTimeout        time.Duration
	TurnTimeout       time.Duration
}

type boxMediaRule struct {
	deviceID       string
	pairingID      string
	roomID         string
	utterance      string
	normalizedText string
	commandID      string
	locale         string
}

// BoxMediaHandler accepts one complete L16 turn over a dedicated HTTPS
// listener. It is intentionally not mounted by Bridge.Mount, keeping the
// existing exactly-four speechkit.device_agent.v1 routes unchanged.
type BoxMediaHandler struct {
	bridge      *Bridge
	localSTT    BoxMediaLocalTranscriber
	mediaToken  string
	rule        boxMediaRule
	sttTimeout  time.Duration
	turnTimeout time.Duration
}

func NewBoxMediaHandler(opts BoxMediaHandlerOptions) (*BoxMediaHandler, error) {
	if !boxMediaBridgeReady(opts.Bridge) {
		return nil, ErrBoxMediaBridgeRequired
	}
	if opts.LocalSTT == nil || !opts.LocalSTT.LocalOnly() {
		return nil, ErrBoxMediaLocalSTTRequired
	}
	rule, err := opts.Bridge.newBoxMediaRule(opts.Rule)
	if err != nil {
		return nil, err
	}
	mediaToken := strings.TrimSpace(opts.MediaPairingToken)
	if !validBridgeToken(mediaToken) || boxMediaTokenReusesBridgeCredential(mediaToken, opts.Bridge.bindings) {
		return nil, ErrBoxMediaTokenInvalid
	}
	sttTimeout, err := boundedBoxMediaTimeout(opts.STTTimeout, boxMediaDefaultSTTTimeout)
	if err != nil {
		return nil, fmt.Errorf("box media: invalid STT timeout: %w", err)
	}
	turnTimeout, err := boundedBoxMediaTimeout(opts.TurnTimeout, boxMediaDefaultTurnTimeout)
	if err != nil {
		return nil, fmt.Errorf("box media: invalid turn timeout: %w", err)
	}
	return &BoxMediaHandler{
		bridge: opts.Bridge, localSTT: opts.LocalSTT, mediaToken: mediaToken, rule: rule,
		sttTimeout: sttTimeout, turnTimeout: turnTimeout,
	}, nil
}

func boxMediaBridgeReady(bridge *Bridge) bool {
	return bridge != nil && validBridgeID(bridge.serverInstanceID) && len(bridge.bindings) > 0 &&
		bridge.ha != nil && bridge.tts != nil && bridge.ttsReady && bridge.claims != nil && bridge.policy != nil &&
		bridge.maxRequestAge > 0 && bridge.futureSkew >= 0 && bridge.now != nil
}

func boxMediaTokenReusesBridgeCredential(mediaToken string, bindings map[string]deviceBinding) bool {
	for _, binding := range bindings {
		if len(mediaToken) == len(binding.token) && subtle.ConstantTimeCompare([]byte(mediaToken), []byte(binding.token)) == 1 {
			return true
		}
	}
	return false
}

func boundedBoxMediaTimeout(value, fallback time.Duration) (time.Duration, error) {
	if value == 0 {
		value = fallback
	}
	if value <= 0 || value > boxMediaMaximumStageTimeout {
		return 0, fmt.Errorf("must be within 1ns..%s", boxMediaMaximumStageTimeout)
	}
	return value, nil
}

func (b *Bridge) newBoxMediaRule(raw BoxMediaRuleOptions) (boxMediaRule, error) {
	rule := boxMediaRule{
		deviceID: strings.TrimSpace(raw.DeviceID), pairingID: strings.TrimSpace(raw.PairingID),
		roomID: strings.TrimSpace(raw.RoomID), commandID: strings.TrimSpace(raw.CommandID),
		locale: strings.TrimSpace(raw.Locale),
	}
	rule.utterance, rule.normalizedText = normalizePolicyText(raw.Transcript)
	if !validBridgeID(rule.deviceID) || !validBridgeID(rule.pairingID) || !validBridgeID(rule.roomID) ||
		!validBridgeID(rule.commandID) || rule.utterance == "" || len(rule.utterance) > maxPolicyTriggerTextBytes {
		return boxMediaRule{}, ErrBoxMediaRuleInvalid
	}
	locale, ok := normalizePolicyLocale(rule.locale)
	if !ok {
		return boxMediaRule{}, ErrBoxMediaRuleInvalid
	}
	rule.locale = locale
	binding, ok := b.bindings[rule.deviceID]
	if !ok || binding.pairingID != rule.pairingID || binding.roomID != rule.roomID {
		return boxMediaRule{}, fmt.Errorf("%w: mapping identity does not match a paired device", ErrBoxMediaRuleInvalid)
	}
	if !boxMediaBindingHasRFC1918Source(binding) {
		return boxMediaRule{}, fmt.Errorf("%w: selected device binding requires one RFC1918 IPv4 source prefix", ErrBoxMediaRuleInvalid)
	}
	policy, ok := b.policy.rules[rule.commandID]
	if !ok || policy.deviceID != rule.deviceID || policy.roomID != rule.roomID ||
		policy.normalizedText != rule.normalizedText || policy.locale != rule.locale {
		return boxMediaRule{}, fmt.Errorf("%w: mapping does not match the existing G0 policy rule", ErrBoxMediaRuleInvalid)
	}
	// Always execute the server-owned policy wording rather than the raw
	// configuration or STT result.
	rule.utterance = policy.utterance
	return rule, nil
}

func boxMediaBindingHasRFC1918Source(binding deviceBinding) bool {
	for _, network := range binding.allowed {
		if network != nil && boxMediaRFC1918CIDR(network.String()) {
			return true
		}
	}
	return false
}

func (h *BoxMediaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.bridge == nil {
		writeBoxMediaError(w, http.StatusServiceUnavailable, "box_media_unavailable", "box_media_not_initialized", false, "no", "Repair the local SpeechKit Box media listener.")
		return
	}
	w.Header().Set(wire.ServerInstanceHeader, h.bridge.serverInstanceID)
	if r.URL.Path != BoxMediaTurnPath {
		writeBoxMediaError(w, http.StatusNotFound, "route_not_found", "box_media_route_unknown", false, "no", "Use the exact Box media turn endpoint.")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeBoxMediaError(w, http.StatusMethodNotAllowed, "method_not_allowed", "box_media_post_required", false, "no", "Send one finite POST request.")
		return
	}
	if r.TLS == nil || r.TLS.Version != tls.VersionTLS13 {
		writeBoxMediaError(w, http.StatusUpgradeRequired, "secure_transport_required", "box_media_tls13_required", false, "no", "Connect directly with HTTPS and the pinned local SpeechKit CA.")
		return
	}
	if len(r.TransferEncoding) != 0 || r.ContentLength < 0 {
		writeBoxMediaError(w, http.StatusLengthRequired, "audio_length_required", "finite_content_length_required", false, "no", "Send one finite L16 body with an exact Content-Length.")
		return
	}
	if strings.TrimSpace(r.Header.Get("Content-Encoding")) != "" {
		writeBoxMediaError(w, http.StatusUnsupportedMediaType, "audio_format_invalid", "content_encoding_forbidden", false, "no", "Send uncompressed audio/L16 bytes.")
		return
	}
	if !validBoxMediaContentType(r.Header.Get("Content-Type")) {
		writeBoxMediaError(w, http.StatusUnsupportedMediaType, "audio_format_invalid", "l16_16khz_mono_required", false, "no", "Send audio/L16 with rate=16000 and channels=1.")
		return
	}
	if r.ContentLength < minBoxMediaInputBytes() || r.ContentLength > maxBoxMediaInputBytes() || r.ContentLength%boxMediaBytesPerSample != 0 {
		writeBoxMediaError(w, http.StatusRequestEntityTooLarge, "audio_duration_invalid", "audio_must_be_300ms_to_15s", false, "no", "Send 300 milliseconds to 15 seconds of 16 kHz mono L16 audio.")
		return
	}

	deviceID := strings.TrimSpace(r.Header.Get(BoxMediaDeviceIDHeader))
	pairingID := strings.TrimSpace(r.Header.Get(BoxMediaPairingIDHeader))
	requestID := strings.TrimSpace(r.Header.Get(BoxMediaRequestIDHeader))
	inputSHA := strings.TrimSpace(r.Header.Get(BoxMediaInputSHA256Header))
	mediaToken, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok || len(mediaToken) != len(h.mediaToken) || subtle.ConstantTimeCompare([]byte(mediaToken), []byte(h.mediaToken)) != 1 {
		writeBoxMediaError(w, http.StatusUnauthorized, "device_auth_required", "pairing_credential_invalid", false, "no", "Pair this device again with the local SpeechKit server.")
		return
	}
	binding, found := h.bridge.bindings[deviceID]
	if !found {
		writeBoxMediaError(w, http.StatusForbidden, "device_binding_denied", "box_media_mapping_mismatch", false, "no", "Use the Box media mapping assigned to this paired device.")
		return
	}
	if pairingID != binding.pairingID {
		writeBoxMediaError(w, http.StatusForbidden, "device_binding_denied", "pairing_id_mismatch", false, "no", "Use the server-issued pairing epoch for this device.")
		return
	}
	if binding.roomID != h.rule.roomID {
		writeBoxMediaError(w, http.StatusForbidden, "device_binding_denied", "box_media_mapping_mismatch", false, "no", "Use the Box media mapping assigned to this paired device.")
		return
	}
	if binding.deviceID != h.rule.deviceID || binding.pairingID != h.rule.pairingID {
		writeBoxMediaError(w, http.StatusForbidden, "device_binding_denied", "box_media_mapping_mismatch", false, "no", "Use the Box media mapping assigned to this paired device.")
		return
	}
	// The direct TCP peer is the only source identity. X-Forwarded-For,
	// Forwarded, X-Real-IP, and proxy metadata are intentionally ignored.
	if !bindingAllowsRemote(binding, r.RemoteAddr) {
		writeBoxMediaError(w, http.StatusForbidden, "device_source_denied", "source_cidr_not_allowed", false, "no", "Connect directly from the paired device network address.")
		return
	}
	if requestID == "" || !validLowerSHA256(inputSHA) {
		writeBoxMediaError(w, http.StatusUnprocessableEntity, "box_media_request_invalid", "request_or_audio_identity_invalid", false, "no", "Send a UUIDv7 request id and lowercase SHA-256 of the exact L16 body.")
		return
	}
	if reason := validateUUIDv7Window(requestID, h.bridge.now().UTC(), h.bridge.maxRequestAge, h.bridge.futureSkew); reason != "" {
		writeBoxMediaError(w, http.StatusUnprocessableEntity, "request_id_invalid", reason, false, "no", "Generate a fresh UUIDv7 request id on the paired Box.")
		return
	}

	body, err := readExactBoxMediaBody(w, r, int(r.ContentLength))
	if err != nil {
		writeBoxMediaError(w, http.StatusBadRequest, "audio_body_invalid", "audio_body_length_mismatch", false, "no", "Send exactly the declared L16 byte count.")
		return
	}
	actualSHA := sha256.Sum256(body)
	expectedSHA, _ := hex.DecodeString(inputSHA)
	if subtle.ConstantTimeCompare(actualSHA[:], expectedSHA) != 1 {
		writeBoxMediaError(w, http.StatusUnprocessableEntity, "audio_digest_mismatch", "audio_sha256_mismatch", false, "no", "Recompute SHA-256 over the exact transmitted L16 bytes.")
		return
	}

	sttCtx, sttCancel := context.WithTimeout(r.Context(), h.sttTimeout)
	transcript, err := h.localSTT.TranscribeLocal(sttCtx, audio.PCMToWAV(l16ToPCM16LE(body)), h.rule.locale)
	sttCancel()
	if err != nil || transcript == nil || strings.TrimSpace(transcript.Provider) == "" || len(strings.TrimSpace(transcript.Provider)) > 128 {
		writeBoxMediaError(w, http.StatusServiceUnavailable, "local_stt_unavailable", "local_stt_failed", true, "no", "Retry after the local SpeechKit STT provider is ready.")
		return
	}
	if len(transcript.Text) > maxPolicyTriggerTextBytes {
		writeBoxMediaError(w, http.StatusForbidden, "local_policy_denied", "box_media_transcript_mismatch", false, "no", "Repeat the exact configured local light phrase.")
		return
	}
	_, normalizedTranscript := normalizePolicyText(transcript.Text)
	if normalizedTranscript == "" || normalizedTranscript != h.rule.normalizedText {
		writeBoxMediaError(w, http.StatusForbidden, "local_policy_denied", "box_media_transcript_mismatch", false, "no", "Repeat the exact configured local light phrase.")
		return
	}

	turnCtx, turnCancel := context.WithTimeout(r.Context(), h.turnTimeout)
	result, err := h.bridge.ExecuteTurn(turnCtx, TurnRequest{
		RequestID: requestID, SessionID: "box-media-" + requestID,
		CommandID: h.rule.commandID, DeviceID: binding.deviceID, PairingID: binding.pairingID,
		// The dedicated media credential never crosses into the four G0
		// routes. The server uses its retained bridge binding internally.
		RoomID: binding.roomID, PairingToken: binding.token, Text: h.rule.utterance,
		Locale: h.rule.locale, InputSHA256: inputSHA,
	})
	turnCancel()
	if err != nil {
		writeBoxMediaExecutionError(w, err)
		return
	}
	pcm, err := boxMediaPCM48k(result)
	if err != nil {
		writeBoxMediaError(w, http.StatusBadGateway, "tts_response_invalid", "tts_pcm_contract_invalid", false, "not_applicable", "Inspect the local TTS provider output.")
		return
	}
	l16 := pcm16LEToL16(pcm)
	outputSHA := sha256.Sum256(l16)
	w.Header().Set("Content-Type", boxMediaContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(l16)))
	w.Header().Set(BoxMediaDeviceIDHeader, binding.deviceID)
	w.Header().Set(BoxMediaRequestIDHeader, requestID)
	w.Header().Set(BoxMediaPairingIDHeader, binding.pairingID)
	w.Header().Set(BoxMediaInputSHA256Header, inputSHA)
	w.Header().Set(BoxMediaOutputSHA256Header, hex.EncodeToString(outputSHA[:]))
	w.Header().Set(BoxMediaReplayHeader, fmt.Sprintf("%t", result.Assist.Replayed))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(l16)
}

func bearerToken(raw string) (string, bool) {
	scheme, token, found := strings.Cut(strings.TrimSpace(raw), " ")
	if !found || !strings.EqualFold(strings.TrimSpace(scheme), "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

func validBoxMediaContentType(raw string) bool {
	mediaType, parameters, err := mime.ParseMediaType(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(mediaType, "audio/L16") || len(parameters) != 2 {
		return false
	}
	return parameters["rate"] == "16000" && parameters["channels"] == "1"
}

func minBoxMediaInputBytes() int64 {
	return int64(boxMediaInputRate*boxMediaBytesPerSample) * boxMediaMinimumDuration.Milliseconds() / 1000
}

func maxBoxMediaInputBytes() int64 {
	return int64(boxMediaInputRate*boxMediaBytesPerSample) * int64(boxMediaMaximumDuration/time.Second)
}

func readExactBoxMediaBody(w http.ResponseWriter, r *http.Request, size int) ([]byte, error) {
	defer r.Body.Close() //nolint:errcheck // the finite request body is fully consumed below
	reader := http.MaxBytesReader(w, r.Body, maxBoxMediaInputBytes()+1)
	body, err := io.ReadAll(reader)
	if err != nil || len(body) != size {
		return nil, errors.New("box media body length mismatch")
	}
	return body, nil
}

func writeBoxMediaExecutionError(w http.ResponseWriter, err error) {
	var executionErr *TurnExecutionError
	if errors.As(err, &executionErr) {
		writeBoxMediaJSON(w, executionErr.StatusCode, executionErr.Envelope)
		return
	}
	writeBoxMediaError(w, http.StatusInternalServerError, "box_media_failed", "box_media_execution_failed", false, "unknown", "Inspect the local SpeechKit server logs before issuing a new request id.")
}

func writeBoxMediaError(w http.ResponseWriter, status int, errorCode, reasonCode string, retryable bool, actionExecuted, guidance string) {
	writeBoxMediaJSON(w, status, wire.ErrorEnvelope{Error: wire.BridgeError{
		ErrorCode: errorCode, ReasonCode: reasonCode, Retryable: retryable,
		ActionExecuted: actionExecuted, UserGuidance: guidance,
	}})
}

func writeBoxMediaJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
