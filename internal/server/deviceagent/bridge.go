package deviceagent

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kombifyio/SpeechKit/internal/server/deviceagent/claimstore"
	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/deviceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

const (
	ClaimDispatchNew     = "dispatch_new"
	ClaimReplayCompleted = "replay_completed"
	ClaimIndeterminate   = "indeterminate"
	ClaimDigestConflict  = "digest_conflict"
	ClaimNotFound        = "not_found"

	maxJSONBodyBytes   = 64 << 10
	maxAssistTextBytes = 4096
	maxTTSAudioBytes   = 8 << 20
	minPairingTokenLen = 32
)

type HomeAssistant interface {
	Probe(context.Context) error
	Converse(context.Context, string, string) (*HomeAssistantResult, error)
	VerifyState(context.Context, string, string) error
}

type TTSSynthesizer interface {
	Synthesize(context.Context, string, tts.SynthesizeOpts) (*tts.Result, error)
	ReadyHealthCheck(context.Context) map[string]error
}

type ClaimKey struct {
	PairingID string
	RequestID string
}

type StoredResult struct {
	Status         string
	ConversationID string
	ResponseType   string
	Speech         string
	Language       string
	ErrorCode      string
	ReasonCode     string
	Retryable      bool
	ActionExecuted string
}

type ClaimDecision struct {
	Disposition string
	Result      *StoredResult
}

// ClaimLedger is a durable at-most-once ledger. Claim must commit a new row
// before returning ClaimDispatchNew. Implementations must never turn an
// existing nonterminal claim back into a dispatch decision.
type ClaimLedger interface {
	Claim(context.Context, ClaimKey, [32]byte, time.Time) (ClaimDecision, error)
	Lookup(context.Context, ClaimKey, time.Time) (ClaimDecision, error)
	Complete(context.Context, ClaimKey, [32]byte, StoredResult, time.Time) error
	MarkIndeterminate(context.Context, ClaimKey, [32]byte, string, time.Time) error
}

type DeviceBindingOptions struct {
	PairingID          string
	DeviceID           string
	RoomID             string
	Token              string
	AllowedClientCIDRs []string
}

type BridgeOptions struct {
	ServerInstanceID   string
	Bindings           []DeviceBindingOptions
	HomeAssistant      HomeAssistant
	TTS                TTSSynthesizer
	TTSReady           bool
	Claims             ClaimLedger
	Policy             *Policy
	MaxRequestAge      time.Duration
	FutureSkew         time.Duration
	ProbeTimeout       time.Duration
	StateVerifyTimeout time.Duration
	Now                func() time.Time
}

type deviceBinding struct {
	pairingID string
	deviceID  string
	roomID    string
	token     string
	allowed   []*net.IPNet
}

type Bridge struct {
	serverInstanceID   string
	bindings           map[string]deviceBinding
	ha                 HomeAssistant
	tts                TTSSynthesizer
	ttsReady           bool
	claims             ClaimLedger
	policy             *Policy
	maxRequestAge      time.Duration
	futureSkew         time.Duration
	probeTimeout       time.Duration
	stateVerifyTimeout time.Duration
	now                func() time.Time
}

func NewBridge(opts BridgeOptions) (*Bridge, error) {
	if !validBridgeID(opts.ServerInstanceID) {
		return nil, errors.New("device-agent bridge: server instance id must be a bounded stable identifier")
	}
	if opts.HomeAssistant == nil {
		return nil, errors.New("device-agent bridge: Home Assistant client is required")
	}
	if opts.TTS == nil || !opts.TTSReady {
		return nil, errors.New("device-agent bridge: ready TTS synthesizer is required")
	}
	if opts.Claims == nil {
		return nil, errors.New("device-agent bridge: durable claim ledger is required")
	}
	if opts.Policy == nil {
		return nil, errors.New("device-agent bridge: local command policy is required")
	}
	if opts.MaxRequestAge <= 0 {
		return nil, errors.New("device-agent bridge: max request age must be positive")
	}
	if opts.FutureSkew < 0 {
		return nil, errors.New("device-agent bridge: future skew must not be negative")
	}
	bindings := make(map[string]deviceBinding, len(opts.Bindings))
	pairingIDs := make(map[string]struct{}, len(opts.Bindings))
	tokens := make(map[string]struct{}, len(opts.Bindings))
	for _, raw := range opts.Bindings {
		binding, err := newDeviceBinding(raw)
		if err != nil {
			return nil, err
		}
		if _, exists := bindings[binding.deviceID]; exists {
			return nil, fmt.Errorf("device-agent bridge: duplicate device id %q", binding.deviceID)
		}
		if _, exists := pairingIDs[binding.pairingID]; exists {
			return nil, fmt.Errorf("device-agent bridge: duplicate pairing id %q", binding.pairingID)
		}
		if _, exists := tokens[binding.token]; exists {
			return nil, errors.New("device-agent bridge: pairing tokens must be unique per device")
		}
		bindings[binding.deviceID] = binding
		pairingIDs[binding.pairingID] = struct{}{}
		tokens[binding.token] = struct{}{}
	}
	if len(bindings) == 0 {
		return nil, errors.New("device-agent bridge: at least one device binding is required")
	}
	probeTimeout := opts.ProbeTimeout
	if probeTimeout <= 0 {
		probeTimeout = 2 * time.Second
	}
	if opts.StateVerifyTimeout < 0 || opts.StateVerifyTimeout > 10*time.Second {
		return nil, errors.New("device-agent bridge: state verification timeout must be between zero and ten seconds")
	}
	stateVerifyTimeout := opts.StateVerifyTimeout
	if stateVerifyTimeout == 0 {
		stateVerifyTimeout = 3 * time.Second
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Bridge{
		serverInstanceID:   strings.TrimSpace(opts.ServerInstanceID),
		bindings:           bindings,
		ha:                 opts.HomeAssistant,
		tts:                opts.TTS,
		ttsReady:           opts.TTSReady,
		claims:             opts.Claims,
		policy:             opts.Policy,
		maxRequestAge:      opts.MaxRequestAge,
		futureSkew:         opts.FutureSkew,
		probeTimeout:       probeTimeout,
		stateVerifyTimeout: stateVerifyTimeout,
		now:                now,
	}, nil
}

func newDeviceBinding(raw DeviceBindingOptions) (deviceBinding, error) {
	binding := deviceBinding{
		pairingID: strings.TrimSpace(raw.PairingID),
		deviceID:  strings.TrimSpace(raw.DeviceID),
		roomID:    strings.TrimSpace(raw.RoomID),
		token:     strings.TrimSpace(raw.Token),
	}
	switch {
	case !validBridgeID(binding.pairingID):
		return deviceBinding{}, errors.New("device-agent bridge: pairing id must be a bounded stable identifier")
	case !validBridgeID(binding.deviceID):
		return deviceBinding{}, errors.New("device-agent bridge: device id must be a bounded stable identifier")
	case !validBridgeID(binding.roomID):
		return deviceBinding{}, fmt.Errorf("device-agent bridge: room id must be a bounded stable identifier for device %q", binding.deviceID)
	case !validBridgeToken(binding.token):
		return deviceBinding{}, fmt.Errorf("device-agent bridge: pairing token for device %q must be a %d..512 byte bearer credential", binding.deviceID, minPairingTokenLen)
	case len(raw.AllowedClientCIDRs) == 0:
		return deviceBinding{}, fmt.Errorf("device-agent bridge: allowed client CIDRs are required for device %q", binding.deviceID)
	}
	for _, rawCIDR := range raw.AllowedClientCIDRs {
		cidr := strings.TrimSpace(rawCIDR)
		if !localBridgeCIDR(cidr) {
			return deviceBinding{}, fmt.Errorf("device-agent bridge: client CIDR %q must be an explicit local range", rawCIDR)
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return deviceBinding{}, fmt.Errorf("device-agent bridge: invalid client CIDR %q: %w", rawCIDR, err)
		}
		binding.allowed = append(binding.allowed, network)
	}
	return binding, nil
}

var bridgeLocalPrefixes = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
}

func localBridgeCIDR(raw string) bool {
	prefix, err := netip.ParsePrefix(raw)
	if err != nil || prefix.Addr().Is4In6() {
		return false
	}
	prefix = prefix.Masked()
	for _, allowed := range bridgeLocalPrefixes {
		if prefix.Addr().BitLen() == allowed.Addr().BitLen() && prefix.Bits() >= allowed.Bits() && allowed.Contains(prefix.Addr()) {
			return true
		}
	}
	return false
}

func validBridgeID(raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '.', character == '_', character == '-', character == ':':
		default:
			return false
		}
	}
	return true
}

func validBridgeToken(value string) bool {
	if len(value) < minPairingTokenLen || len(value) > 512 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-', character == '.', character == '_', character == '~':
		case character == '+', character == '/', character == '=':
		default:
			return false
		}
	}
	return true
}

func (b *Bridge) Mount(mux *http.ServeMux) {
	if b == nil || mux == nil {
		return
	}
	mux.HandleFunc("/v1/device-agent/register", b.wrap(b.register))
	mux.HandleFunc("/v1/device-agent/events", b.wrap(b.events))
	mux.HandleFunc("/v1/device-agent/assist", b.wrap(b.assist))
	mux.HandleFunc("/v1/device-agent/tts", b.wrap(b.synthesize))
}

func (b *Bridge) wrap(next func(http.ResponseWriter, *http.Request, deviceBinding)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(wire.ServerInstanceHeader, b.serverInstanceID)
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			b.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "device_agent_post_required", false, "no", "Use POST for this local device-agent endpoint.")
			return
		}
		binding, ok := b.authenticate(r)
		if !ok {
			b.writeError(w, http.StatusUnauthorized, "device_auth_required", "pairing_credential_invalid", false, "no", "Pair this device again with the local SpeechKit server.")
			return
		}
		if !bindingAllowsRemote(binding, r.RemoteAddr) {
			b.writeError(w, http.StatusForbidden, "device_source_denied", "source_cidr_not_allowed", false, "no", "Connect directly from the paired device network address.")
			return
		}
		next(w, r, binding)
	}
}

func (b *Bridge) authenticate(r *http.Request) (deviceBinding, bool) {
	deviceID := strings.TrimSpace(r.Header.Get("X-SpeechKit-Device-ID"))
	binding, ok := b.bindings[deviceID]
	if !ok {
		return deviceBinding{}, false
	}
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, token, found := strings.Cut(authorization, " ")
	if !found || !strings.EqualFold(strings.TrimSpace(scheme), "Bearer") {
		return deviceBinding{}, false
	}
	token = strings.TrimSpace(token)
	if len(token) != len(binding.token) || subtle.ConstantTimeCompare([]byte(token), []byte(binding.token)) != 1 {
		return deviceBinding{}, false
	}
	return binding, true
}

func bindingAllowsRemote(binding deviceBinding, remoteAddr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, network := range binding.allowed {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (b *Bridge) register(w http.ResponseWriter, r *http.Request, binding deviceBinding) {
	var body wire.Registration
	if !b.decode(w, r, &body) {
		return
	}
	if body.Version != wire.CurrentProtocolVersion {
		b.writeError(w, http.StatusUnprocessableEntity, "protocol_version_unsupported", "device_agent_protocol_mismatch", false, "no", "Update the device agent to the server-supported protocol version.")
		return
	}
	if !b.requireBoundDevice(w, binding, body.Device.DeviceID, body.Device.RoomID) {
		return
	}
	haCapability := wire.CapabilityState{Status: wire.CapabilityUnavailable, ReasonCode: "ha_probe_unavailable"}
	probeCtx, cancel := context.WithTimeout(r.Context(), b.probeTimeout)
	probeErr := b.ha.Probe(probeCtx)
	cancel()
	if probeErr == nil {
		haCapability = wire.CapabilityState{Status: wire.CapabilityReady}
	} else {
		var dispatchErr *HomeAssistantDispatchError
		if errors.As(probeErr, &dispatchErr) && dispatchErr.ReasonCode != "" {
			haCapability.ReasonCode = dispatchErr.ReasonCode
		}
	}
	ttsCapability := wire.CapabilityState{Status: wire.CapabilityUnavailable, ReasonCode: "tts_unavailable"}
	if b.tts != nil && b.ttsReady {
		ttsCtx, ttsCancel := context.WithTimeout(r.Context(), b.probeTimeout)
		health := b.tts.ReadyHealthCheck(ttsCtx)
		ttsCancel()
		for _, healthErr := range health {
			if healthErr == nil {
				ttsCapability = wire.CapabilityState{Status: wire.CapabilityReady}
				break
			}
		}
		if ttsCapability.Status != wire.CapabilityReady && len(health) > 0 {
			ttsCapability.ReasonCode = "tts_probe_unavailable"
		}
	}
	b.writeJSON(w, http.StatusOK, wire.RegistrationAck{
		Status:           "paired",
		PairingID:        binding.pairingID,
		ServerInstanceID: b.serverInstanceID,
		ServerTime:       b.now().UTC().Format(time.RFC3339Nano),
		Capabilities: wire.BridgeCapabilities{
			HomeAssistant: haCapability,
			TTS:           ttsCapability,
		},
	})
}

func (b *Bridge) events(w http.ResponseWriter, r *http.Request, binding deviceBinding) {
	var body wire.Event
	if !b.decode(w, r, &body) {
		return
	}
	if !b.requireBoundDevice(w, binding, body.DeviceID, body.RoomID) {
		return
	}
	if !allowedEventType(body.Type) || body.Surface != "device_agent" || body.Transport != "local_http" {
		b.writeError(w, http.StatusUnprocessableEntity, "event_rejected", "device_event_contract_invalid", false, "not_applicable", "Send only documented local device-agent lifecycle events.")
		return
	}
	if len(body.SessionID) > 128 || len(body.RequestID) > 64 || len(body.Text) > maxAssistTextBytes || len(body.SpeakText) > maxAssistTextBytes {
		b.writeError(w, http.StatusRequestEntityTooLarge, "event_rejected", "device_event_too_large", false, "not_applicable", "Reduce event metadata to the documented bounds.")
		return
	}
	// Never log Text, SpeakText, tokens, or arbitrary metadata.
	slog.Info("device-agent lifecycle event",
		"event_type", body.Type,
		"device_id", binding.deviceID,
		"room_id", binding.roomID,
		"request_id", body.RequestID,
		"session_id", body.SessionID)
	b.writeJSON(w, http.StatusAccepted, wire.EventAck{Status: "accepted"})
}

func (b *Bridge) assist(w http.ResponseWriter, r *http.Request, binding deviceBinding) {
	var body wire.AssistRequest
	if !b.decode(w, r, &body) {
		return
	}
	if !b.requireBoundDevice(w, binding, body.DeviceID, body.RoomID) {
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	body.CommandID = strings.TrimSpace(body.CommandID)
	body.Locale = strings.TrimSpace(body.Locale)
	body.SessionID = strings.TrimSpace(body.SessionID)
	if body.Text == "" || len(body.Text) > maxAssistTextBytes || body.CommandID == "" || len(body.CommandID) > 128 || body.Locale == "" || len(body.Locale) > 64 || body.SessionID == "" || len(body.SessionID) > 128 {
		b.writeError(w, http.StatusUnprocessableEntity, "assist_request_invalid", "assist_request_fields_invalid", false, "no", "Provide bounded command_id, text, locale, and session_id fields.")
		return
	}
	now := b.now().UTC()
	if reason := validateUUIDv7Window(body.RequestID, now, b.maxRequestAge, b.futureSkew); reason != "" {
		b.writeError(w, http.StatusUnprocessableEntity, "request_id_invalid", reason, false, "no", "Generate a fresh UUIDv7 request_id on the paired device.")
		return
	}
	command, denial := b.policy.Authorize(binding.deviceID, binding.roomID, body.CommandID, body.Text, body.Locale, now)
	if denial != nil {
		b.writeError(w, http.StatusForbidden, denial.ErrorCode, denial.ReasonCode, false, "no", denial.UserGuidance)
		return
	}
	key := ClaimKey{PairingID: binding.pairingID, RequestID: body.RequestID}
	digest, err := assistDigest(binding.token, key, command)
	if err != nil {
		b.writeError(w, http.StatusUnprocessableEntity, "assist_request_invalid", "assist_request_digest_invalid", false, "no", "Use the paired device identity and bounded documented request fields.")
		return
	}
	decision, err := b.claims.Claim(r.Context(), key, digest, now)
	if err != nil {
		b.writeError(w, http.StatusServiceUnavailable, "claim_store_unavailable", "durable_claim_failed", false, "no", "The local safety ledger is unavailable; retry after repairing server storage.")
		return
	}
	switch decision.Disposition {
	case ClaimReplayCompleted:
		if decision.Result == nil {
			b.writeError(w, http.StatusInternalServerError, "claim_store_invalid", "completed_result_missing", false, "unknown", "Inspect the local safety ledger before issuing another command.")
			return
		}
		b.writeStoredResult(w, body.RequestID, *decision.Result, true)
		return
	case ClaimIndeterminate:
		b.writeError(w, http.StatusConflict, "request_outcome_indeterminate", "prior_dispatch_outcome_unknown", false, "unknown", "Verify the Home Assistant state manually before issuing a new command.")
		return
	case ClaimDigestConflict:
		b.writeError(w, http.StatusConflict, "request_conflict", "request_digest_mismatch", false, "no", "Use a new UUIDv7 request_id for different command content.")
		return
	case ClaimDispatchNew:
		// The durable claim is committed. This is the only branch allowed to
		// dispatch to HA, and it never retries.
	default:
		b.writeError(w, http.StatusInternalServerError, "claim_store_invalid", "claim_disposition_unknown", false, "no", "Inspect the local safety ledger configuration.")
		return
	}

	haResult, dispatchErr := b.ha.Converse(r.Context(), command.Utterance, command.Locale)
	if dispatchErr != nil {
		var classified *HomeAssistantDispatchError
		if !errors.As(dispatchErr, &classified) || classified.ActionExecuted == "unknown" {
			reason := "ha_dispatch_indeterminate"
			if classified != nil && classified.ReasonCode != "" {
				reason = classified.ReasonCode
			}
			_ = b.claims.MarkIndeterminate(r.Context(), key, digest, reason, b.now().UTC())
			b.writeError(w, http.StatusBadGateway, "home_assistant_unavailable", reason, false, "unknown", "Verify the Home Assistant state manually before issuing a new command.")
			return
		}
		stored := StoredResult{
			Status:         "denied",
			Language:       command.Locale,
			ErrorCode:      "home_assistant_rejected",
			ReasonCode:     classified.ReasonCode,
			Retryable:      classified.Retryable,
			ActionExecuted: "no",
		}
		if err := b.claims.Complete(r.Context(), key, digest, stored, b.now().UTC()); err != nil {
			b.writeError(w, http.StatusInternalServerError, "claim_commit_failed", "result_commit_indeterminate", false, "unknown", "Verify the Home Assistant state manually before issuing another command.")
			return
		}
		b.writeStoredResult(w, body.RequestID, stored, false)
		return
	}
	if haResult == nil {
		_ = b.claims.MarkIndeterminate(r.Context(), key, digest, "ha_response_missing", b.now().UTC())
		b.writeError(w, http.StatusBadGateway, "home_assistant_unavailable", "ha_response_missing", false, "unknown", "Verify the Home Assistant state manually before issuing a new command.")
		return
	}
	responseType := strings.ToLower(strings.TrimSpace(haResult.ResponseType))
	if responseType == "error" && haResult.ActionExecuted == claimstore.ActionExecutedNo {
		stored := StoredResult{
			Status:         "denied",
			ConversationID: haResult.ConversationID,
			ResponseType:   responseType,
			Speech:         boundedString(haResult.Speech, 8192),
			Language:       command.Locale,
			ErrorCode:      firstNonEmpty(haResult.ErrorCode, "home_assistant_rejected"),
			ReasonCode:     firstNonEmpty(haResult.ReasonCode, "ha_request_rejected"),
			Retryable:      false,
			ActionExecuted: claimstore.ActionExecutedNo,
		}
		if err := b.claims.Complete(r.Context(), key, digest, stored, b.now().UTC()); err != nil {
			b.writeError(w, http.StatusInternalServerError, "claim_commit_failed", "result_commit_indeterminate", false, "unknown", "Verify the Home Assistant state manually before issuing another command.")
			return
		}
		b.writeStoredResult(w, body.RequestID, stored, false)
		return
	}
	if responseType != "action_done" {
		reason := firstNonEmpty(haResult.ReasonCode, "ha_response_semantics_unknown")
		_ = b.claims.MarkIndeterminate(r.Context(), key, digest, reason, b.now().UTC())
		b.writeError(w, http.StatusBadGateway, "home_assistant_response_indeterminate", reason, false, "unknown", "Verify the Home Assistant state manually before issuing a new command.")
		return
	}
	if !exactAuthorizedTarget(haResult.SuccessTargets, haResult.FailedTargets, command.EntityID) {
		reason := "ha_authorized_target_unverified"
		_ = b.claims.MarkIndeterminate(r.Context(), key, digest, reason, b.now().UTC())
		b.writeError(w, http.StatusBadGateway, "home_assistant_response_indeterminate", reason, false, "unknown", "Verify the Home Assistant state manually before issuing a new command.")
		return
	}
	verifyCtx, cancel := context.WithTimeout(r.Context(), b.stateVerifyTimeout)
	verifyErr := b.ha.VerifyState(verifyCtx, command.EntityID, command.ExpectedState)
	cancel()
	if verifyErr != nil {
		reason := "ha_state_verification_failed"
		var classified *HomeAssistantDispatchError
		if errors.As(verifyErr, &classified) && classified.ReasonCode != "" {
			reason = classified.ReasonCode
		}
		_ = b.claims.MarkIndeterminate(r.Context(), key, digest, reason, b.now().UTC())
		b.writeError(w, http.StatusBadGateway, "home_assistant_response_indeterminate", reason, false, "unknown", "Verify the Home Assistant state manually before issuing a new command.")
		return
	}
	stored := StoredResult{
		Status:         "success",
		ConversationID: haResult.ConversationID,
		ResponseType:   responseType,
		Speech:         boundedString(haResult.Speech, 8192),
		Language:       command.Locale,
		ErrorCode:      "",
		ReasonCode:     "",
		Retryable:      false,
		ActionExecuted: claimstore.ActionExecutedYes,
	}
	if stored.Speech == "" {
		stored.Status = "denied"
		stored.ErrorCode = "home_assistant_response_invalid"
		stored.ReasonCode = "ha_spoken_response_missing"
	}
	if err := b.claims.Complete(r.Context(), key, digest, stored, b.now().UTC()); err != nil {
		b.writeError(w, http.StatusInternalServerError, "claim_commit_failed", "result_commit_indeterminate", false, "unknown", "Verify the Home Assistant state manually before issuing another command.")
		return
	}
	b.writeStoredResult(w, body.RequestID, stored, false)
}

func exactAuthorizedTarget(success, failed []HomeAssistantTarget, entityID string) bool {
	if len(success) != 1 || len(failed) != 0 {
		return false
	}
	target := success[0]
	return target.Type == "entity" && target.ID == entityID
}

func (b *Bridge) synthesize(w http.ResponseWriter, r *http.Request, binding deviceBinding) {
	var body wire.TTSRequest
	if !b.decode(w, r, &body) {
		return
	}
	body.RequestID = strings.TrimSpace(body.RequestID)
	body.Format = strings.ToLower(strings.TrimSpace(body.Format))
	now := b.now().UTC()
	if body.RequestID == "" || (body.Format != "" && body.Format != "wav") {
		b.writeError(w, http.StatusUnprocessableEntity, "tts_request_invalid", "tts_fields_invalid", false, "not_applicable", "Provide the completed assist request_id and request WAV output.")
		return
	}
	if reason := validateUUIDv7Window(body.RequestID, now, b.maxRequestAge, b.futureSkew); reason != "" {
		b.writeError(w, http.StatusUnprocessableEntity, "request_id_invalid", reason, false, "not_applicable", "Use the UUIDv7 request_id returned by the completed Assist call.")
		return
	}
	decision, err := b.claims.Lookup(r.Context(), ClaimKey{PairingID: binding.pairingID, RequestID: body.RequestID}, now)
	if err != nil {
		b.writeError(w, http.StatusServiceUnavailable, "claim_store_unavailable", "durable_lookup_failed", false, "not_applicable", "Retry after repairing the local safety ledger.")
		return
	}
	if decision.Disposition == ClaimNotFound {
		b.writeError(w, http.StatusNotFound, "tts_source_not_found", "assist_result_not_found", false, "not_applicable", "Use a completed Assist request from this pairing epoch.")
		return
	}
	if decision.Disposition == ClaimIndeterminate {
		b.writeError(w, http.StatusConflict, "request_outcome_indeterminate", "prior_dispatch_outcome_unknown", false, "unknown", "Verify the Home Assistant state manually; TTS cannot be generated for this request.")
		return
	}
	if decision.Disposition != ClaimReplayCompleted || decision.Result == nil {
		b.writeError(w, http.StatusInternalServerError, "claim_store_invalid", "tts_source_invalid", false, "unknown", "Inspect the local safety ledger before requesting TTS.")
		return
	}
	stored := *decision.Result
	if strings.TrimSpace(stored.Speech) == "" || strings.TrimSpace(stored.Language) == "" {
		b.writeError(w, http.StatusUnprocessableEntity, "tts_source_invalid", "assist_speech_missing", false, firstNonEmpty(stored.ActionExecuted, "not_applicable"), "The completed Home Assistant result has no safe spoken response.")
		return
	}
	result, err := b.tts.Synthesize(r.Context(), stored.Speech, tts.SynthesizeOpts{Locale: stored.Language, Format: "wav"})
	if err != nil || result == nil {
		b.writeError(w, http.StatusServiceUnavailable, "tts_unavailable", "local_tts_failed", true, "not_applicable", "Retry TTS; the Home Assistant command will not be repeated.")
		return
	}
	format := strings.ToLower(strings.TrimSpace(result.Format))
	provider := strings.TrimSpace(result.Provider)
	if len(result.Audio) == 0 || len(result.Audio) > maxTTSAudioBytes || format != "wav" || result.SampleRate <= 0 || result.SampleRate > 384000 || provider == "" || len(provider) > 128 {
		b.writeError(w, http.StatusBadGateway, "tts_response_invalid", "tts_audio_contract_invalid", false, "not_applicable", "Inspect the local TTS provider.")
		return
	}
	b.writeJSON(w, http.StatusOK, wire.TTSResponse{
		RequestID:   body.RequestID,
		AudioBase64: base64.StdEncoding.EncodeToString(result.Audio),
		Format:      format,
		SampleRate:  result.SampleRate,
		DurationMS:  result.Duration.Milliseconds(),
		Provider:    provider,
		Voice:       result.Voice,
	})
}

func (b *Bridge) requireBoundDevice(w http.ResponseWriter, binding deviceBinding, deviceID, roomID string) bool {
	if strings.TrimSpace(deviceID) != binding.deviceID {
		b.writeError(w, http.StatusForbidden, "device_binding_denied", "device_id_mismatch", false, "no", "Use the device id assigned during local pairing.")
		return false
	}
	if strings.TrimSpace(roomID) != binding.roomID {
		b.writeError(w, http.StatusForbidden, "device_binding_denied", "room_id_mismatch", false, "no", "Use the server-assigned room id for this paired device.")
		return false
	}
	return true
}

func (b *Bridge) decode(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close() //nolint:errcheck // bounded request body is fully consumed
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		b.writeError(w, http.StatusBadRequest, "invalid_json", "request_body_invalid", false, "no", "Send one JSON object containing only documented fields.")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		b.writeError(w, http.StatusBadRequest, "invalid_json", "multiple_json_values", false, "no", "Send exactly one JSON object.")
		return false
	}
	return true
}

func (b *Bridge) writeStoredResult(w http.ResponseWriter, requestID string, result StoredResult, replayed bool) {
	if result.Status == "denied" && result.Speech == "" {
		b.writeError(w, http.StatusBadGateway, firstNonEmpty(result.ErrorCode, "home_assistant_rejected"), firstNonEmpty(result.ReasonCode, "ha_request_rejected"), result.Retryable, firstNonEmpty(result.ActionExecuted, "no"), "Review Home Assistant permissions or command details before using a new request id.")
		return
	}
	b.writeJSON(w, http.StatusOK, wire.AssistResponse{
		Status:         result.Status,
		RequestID:      requestID,
		ConversationID: result.ConversationID,
		ResponseType:   result.ResponseType,
		Speech:         result.Speech,
		ActionExecuted: result.ActionExecuted,
		Replayed:       replayed,
		ErrorCode:      result.ErrorCode,
		ReasonCode:     result.ReasonCode,
		Retryable:      result.Retryable,
		UserGuidance:   resultGuidance(result),
	})
}

func (b *Bridge) writeError(w http.ResponseWriter, status int, errorCode, reasonCode string, retryable bool, actionExecuted, guidance string) {
	b.writeJSON(w, status, wire.ErrorEnvelope{Error: wire.BridgeError{
		ErrorCode:      errorCode,
		ReasonCode:     reasonCode,
		Retryable:      retryable,
		ActionExecuted: actionExecuted,
		UserGuidance:   guidance,
	}})
}

func (*Bridge) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func validateUUIDv7Window(raw string, now time.Time, maxAge, futureSkew time.Duration) string {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id.Version() != 7 {
		return "request_id_not_uuidv7"
	}
	seconds, nanoseconds := id.Time().UnixTime()
	issuedAt := time.Unix(seconds, nanoseconds)
	if issuedAt.After(now.Add(futureSkew)) {
		return "request_id_from_future"
	}
	if issuedAt.Before(now.Add(-maxAge)) {
		return "request_id_too_old"
	}
	return ""
}

func assistDigest(pairingToken string, key ClaimKey, command AuthorizedCommand) ([32]byte, error) {
	digest, err := claimstore.HMACDigest([]byte(pairingToken), claimstore.CanonicalRequest{
		PairedDeviceID: key.PairingID,
		RequestID:      key.RequestID,
		RuleID:         command.RuleID,
		Locale:         strings.ToLower(strings.TrimSpace(command.Locale)),
		Text:           strings.TrimSpace(command.Utterance),
		EntityID:       command.EntityID,
		ExpectedState:  command.ExpectedState,
	})
	if err != nil {
		return [32]byte{}, err
	}
	return [32]byte(digest), nil
}

func allowedEventType(value string) bool {
	switch value {
	case "device.wake_detected", "voice.capture_started", "voice.capture_stopped",
		"voice.assist_result", "voice.tts_started", "voice.tts_finished":
		return true
	default:
		return false
	}
}

func boundedString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func resultGuidance(result StoredResult) string {
	if result.Status == "denied" {
		return "Follow the Home Assistant response; do not ask a general LLM to reinterpret the command."
	}
	return ""
}
