// Package deviceagent implements the credential-minimal LAN-side SpeechKit
// device-agent client and its versioned wire contract.
package deviceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

const (
	// CurrentProtocolVersion is the only version emitted or accepted by the
	// credential-minimal local bridge. The v0 protocol constant was removed
	// in v0.68.0; v0 clients never had server-owned pairing or authority
	// guarantees.
	CurrentProtocolVersion   = "speechkit.device_agent.v1"
	defaultLocale            = "de-DE"
	defaultUserAgent         = "speechkit-device-agent/1.0"
	minimumPairingTokenBytes = 32
	maxJSONResponseBytes     = 2 << 20
	maxTTSResponseBytes      = 12 << 20
	maxErrorResponseBytes    = 64 << 10

	CapabilityReady       = "ready"
	CapabilityUnavailable = "unavailable"
	CapabilityUnverified  = "unverified"
	ServerInstanceHeader  = "X-SpeechKit-Server-Instance-ID"
)

var (
	ErrMissingServerURL               = errors.New("speechkit deviceagent: server_url is required")
	ErrMissingPairingToken            = errors.New("speechkit deviceagent: pairing_token is required")
	ErrPairingTokenTooShort           = errors.New("speechkit deviceagent: pairing_token must contain at least 32 bytes")
	ErrPairingTokenInvalid            = errors.New("speechkit deviceagent: pairing_token must be a bounded bearer credential")
	ErrMissingExpectedServerInstance  = errors.New("speechkit deviceagent: expected_server_instance_id is required")
	ErrMissingExpectedPairingID       = errors.New("speechkit deviceagent: expected_pairing_id is required")
	ErrMissingAssistText              = errors.New("speechkit deviceagent: assist text is required")
	ErrMissingCommandID               = errors.New("speechkit deviceagent: command_id is required")
	ErrServerIdentityMismatch         = errors.New("speechkit deviceagent: server instance identity mismatch")
	ErrServerIdentityMissing          = errors.New("speechkit deviceagent: server instance identity header is missing")
	ErrPairingIdentityMismatch        = errors.New("speechkit deviceagent: pairing epoch identity mismatch")
	ErrAssistResponseMismatch         = errors.New("speechkit deviceagent: assist response does not match the request")
	ErrResponseTooLarge               = errors.New("speechkit deviceagent: server response exceeds the protocol limit")
	ErrInsecureServerTransport        = errors.New("speechkit deviceagent: plaintext HTTP is allowed only for a loopback server")
	ErrHomeAssistantBridgeUnavailable = errors.New("speechkit deviceagent: Home Assistant bridge is not ready")
	ErrTTSBridgeUnavailable           = errors.New("speechkit deviceagent: TTS bridge is not ready")
)

// Config describes a v1 device agent. Authentication is PairingToken only;
// Home Assistant authority, credentials and the HTTP transport policy are
// server-owned and cannot be supplied by the device.
type Config struct {
	ServerURL                string
	PairingToken             string
	ExpectedServerInstanceID string
	ExpectedPairingID        string
	UserAgent                string
	Device                   DeviceDescriptor
	Capabilities             Capabilities
	Health                   Health
	Locale                   string
}

type DeviceDescriptor struct {
	AgentID       string      `json:"agent_id"`
	DeviceID      string      `json:"device_id"`
	DisplayName   string      `json:"display_name,omitempty"`
	RoomID        string      `json:"room_id"`
	CaptureDevice AudioDevice `json:"capture_device"`
	OutputDevice  AudioDevice `json:"output_device"`
	Wakeword      Wakeword    `json:"wakeword"`
}

type AudioDevice struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind"` // microphone | speaker
	Transport string `json:"transport,omitempty"`
}

type Wakeword struct {
	Enabled bool   `json:"enabled"`
	Phrase  string `json:"phrase,omitempty"`
	Backend string `json:"backend,omitempty"`
	Status  string `json:"status"`
}

// Capabilities are device-reported facts. Pairing and server-side bridge
// readiness are deliberately absent: only the server can attest those.
type Capabilities struct {
	Dictation     bool `json:"dictation"`
	Assist        bool `json:"assist"`
	VoiceAgent    bool `json:"voice_agent"`
	WakewordLocal bool `json:"wakeword_local"`
	TTS           bool `json:"tts"`
	BargeIn       bool `json:"barge_in"`
}

type Health struct {
	Status       string `json:"status"`
	CaptureReady bool   `json:"capture_ready"`
	OutputReady  bool   `json:"output_ready"`
	WakeReady    bool   `json:"wake_ready"`
}

// Registration is the device-asserted half of the v1 handshake. Pairing state
// is never part of it; the server attests pairing in RegistrationAck.
type Registration struct {
	Version      string           `json:"version"`
	RegisteredAt time.Time        `json:"registered_at"`
	Device       DeviceDescriptor `json:"device"`
	Capabilities Capabilities     `json:"capabilities"`
	Health       Health           `json:"health"`
}

type CapabilityState struct {
	Status     string `json:"status"`
	ReasonCode string `json:"reason_code,omitempty"`
}

type BridgeCapabilities struct {
	HomeAssistant CapabilityState `json:"home_assistant"`
	TTS           CapabilityState `json:"tts"`
}

type RegistrationAck struct {
	Status           string             `json:"status"`
	PairingID        string             `json:"pairing_id"`
	ServerInstanceID string             `json:"server_instance_id"`
	ServerTime       string             `json:"server_time,omitempty"`
	Capabilities     BridgeCapabilities `json:"capabilities"`
}

type Event struct {
	Type          string            `json:"type"`
	Surface       string            `json:"surface"`
	Mode          string            `json:"mode"`
	RequestID     string            `json:"request_id,omitempty"`
	SessionID     string            `json:"session_id,omitempty"`
	DeviceID      string            `json:"device_id"`
	RoomID        string            `json:"room_id"`
	CapturePolicy string            `json:"capture_policy,omitempty"`
	Transport     string            `json:"transport,omitempty"`
	Text          string            `json:"text,omitempty"`
	SpeakText     string            `json:"speak_text,omitempty"`
	ReasonCode    string            `json:"reason_code,omitempty"`
	At            time.Time         `json:"at"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type EventAck struct {
	Status string `json:"status"`
}

type AssistRequest struct {
	RequestID string `json:"request_id"`
	SessionID string `json:"session_id"`
	CommandID string `json:"command_id"`
	DeviceID  string `json:"device_id"`
	RoomID    string `json:"room_id"`
	Text      string `json:"text"`
	Locale    string `json:"locale"`
}

type AssistResponse struct {
	Status         string `json:"status"`
	RequestID      string `json:"request_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	ResponseType   string `json:"response_type,omitempty"`
	Speech         string `json:"speech,omitempty"`
	ActionExecuted string `json:"action_executed"` // yes | no | unknown
	Replayed       bool   `json:"replayed"`
	ErrorCode      string `json:"error_code,omitempty"`
	ReasonCode     string `json:"reason_code,omitempty"`
	Retryable      bool   `json:"retryable"`
	UserGuidance   string `json:"user_guidance,omitempty"`
}

type TTSRequest struct {
	RequestID string `json:"request_id"`
	Format    string `json:"format"`
}

type TTSResponse struct {
	RequestID   string `json:"request_id"`
	AudioBase64 string `json:"audio_base64"`
	Format      string `json:"format"`
	SampleRate  int    `json:"sample_rate"`
	DurationMS  int64  `json:"duration_ms"`
	Provider    string `json:"provider"`
	Voice       string `json:"voice,omitempty"`
}

type ErrorEnvelope struct {
	Error BridgeError `json:"error"`
}

type BridgeError struct {
	ErrorCode      string `json:"error_code"`
	ReasonCode     string `json:"reason_code"`
	Retryable      bool   `json:"retryable"`
	ActionExecuted string `json:"action_executed"`
	UserGuidance   string `json:"user_guidance"`
}

type HTTPError struct {
	Method     string
	Path       string
	StatusCode int
	Envelope   ErrorEnvelope
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "speechkit deviceagent: nil HTTP error"
	}
	code := strings.TrimSpace(e.Envelope.Error.ErrorCode)
	if code != "" {
		return fmt.Sprintf("%s %s returned %d (%s/%s)", e.Method, e.Path, e.StatusCode, code, e.Envelope.Error.ReasonCode)
	}
	return fmt.Sprintf("%s %s returned %d without a structured device-agent error", e.Method, e.Path, e.StatusCode)
}

type CycleOptions struct {
	RequestID string
	SessionID string
	CommandID string
	Text      string
	Locale    string
}

type CycleResult struct {
	RequestID      string  `json:"request_id"`
	SessionID      string  `json:"session_id"`
	SpokenText     string  `json:"spoken_text"`
	ConversationID string  `json:"conversation_id,omitempty"`
	ResponseType   string  `json:"response_type,omitempty"`
	TTSProvider    string  `json:"tts_provider,omitempty"`
	Replayed       bool    `json:"replayed"`
	Events         []Event `json:"events"`
}

type Agent struct {
	cfg       Config
	serverURL *url.URL
	http      *http.Client
	userAgent string
}

func New(cfg Config) (*Agent, error) {
	serverURL, err := parseLocalBaseURL(cfg.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMissingServerURL, err)
	}
	if strings.TrimSpace(cfg.PairingToken) == "" {
		return nil, ErrMissingPairingToken
	}
	if strings.TrimSpace(cfg.ExpectedServerInstanceID) == "" {
		return nil, ErrMissingExpectedServerInstance
	}
	if strings.TrimSpace(cfg.ExpectedPairingID) == "" {
		return nil, ErrMissingExpectedPairingID
	}
	if len(strings.TrimSpace(cfg.PairingToken)) < minimumPairingTokenBytes {
		return nil, ErrPairingTokenTooShort
	}
	if !validPairingToken(strings.TrimSpace(cfg.PairingToken)) {
		return nil, ErrPairingTokenInvalid
	}
	validation := localValidation()
	httpClient := netsec.NewSafeHTTPClient(netsec.ClientOptions{
		Timeout:        10 * time.Second,
		DialValidation: &validation,
	})
	clientCopy := *httpClient
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	userAgent := strings.TrimSpace(cfg.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	cfg.Locale = firstNonEmpty(cfg.Locale, defaultLocale)
	cfg.PairingToken = strings.TrimSpace(cfg.PairingToken)
	cfg.ExpectedServerInstanceID = strings.TrimSpace(cfg.ExpectedServerInstanceID)
	cfg.ExpectedPairingID = strings.TrimSpace(cfg.ExpectedPairingID)
	cfg.Device = normalizeDevice(cfg.Device)
	cfg.Health = normalizeHealth(cfg.Health)
	return &Agent{cfg: cfg, serverURL: serverURL, http: &clientCopy, userAgent: userAgent}, nil
}

func (a *Agent) Register(ctx context.Context) (*RegistrationAck, error) {
	reg := Registration{
		Version:      CurrentProtocolVersion,
		RegisteredAt: time.Now().UTC(),
		Device:       a.cfg.Device,
		Capabilities: a.cfg.Capabilities,
		Health:       a.cfg.Health,
	}
	var ack RegistrationAck
	if err := a.postServerJSON(ctx, "/v1/device-agent/register", reg, &ack); err != nil {
		return nil, err
	}
	if strings.TrimSpace(ack.ServerInstanceID) != a.cfg.ExpectedServerInstanceID {
		return nil, fmt.Errorf("%w: expected %q, got %q", ErrServerIdentityMismatch, a.cfg.ExpectedServerInstanceID, ack.ServerInstanceID)
	}
	if ack.Status != "paired" {
		return nil, fmt.Errorf("speechkit deviceagent: registration status %q is not paired", ack.Status)
	}
	if strings.TrimSpace(ack.PairingID) != a.cfg.ExpectedPairingID {
		return nil, fmt.Errorf("%w: expected %q, got %q", ErrPairingIdentityMismatch, a.cfg.ExpectedPairingID, ack.PairingID)
	}
	return &ack, nil
}

func (a *Agent) RunFakeAssistCycle(ctx context.Context, opts CycleOptions) (*CycleResult, error) {
	text := strings.TrimSpace(opts.Text)
	if text == "" {
		return nil, ErrMissingAssistText
	}
	commandID := strings.TrimSpace(opts.CommandID)
	if commandID == "" {
		return nil, ErrMissingCommandID
	}
	requestID := strings.TrimSpace(opts.RequestID)
	if requestID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("generate UUIDv7 request id: %w", err)
		}
		requestID = id.String()
	}
	sessionID := firstNonEmpty(opts.SessionID, "device-agent-"+requestID)
	locale := firstNonEmpty(opts.Locale, a.cfg.Locale, defaultLocale)
	events := make([]Event, 0, 6)

	ack, err := a.Register(ctx)
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}
	if ack.Capabilities.HomeAssistant.Status != CapabilityReady {
		return nil, fmt.Errorf("%w: %s", ErrHomeAssistantBridgeUnavailable, ack.Capabilities.HomeAssistant.ReasonCode)
	}
	if ack.Capabilities.TTS.Status != CapabilityReady {
		return nil, fmt.Errorf("%w: %s", ErrTTSBridgeUnavailable, ack.Capabilities.TTS.ReasonCode)
	}
	publish := func(ev Event) error {
		ev = a.fillEventDefaults(ev, requestID, sessionID)
		if err := a.Publish(ctx, ev); err != nil {
			return err
		}
		events = append(events, ev)
		return nil
	}

	if err := publish(Event{Type: "device.wake_detected", Mode: "assist", Text: a.cfg.Device.Wakeword.Phrase}); err != nil {
		return nil, fmt.Errorf("publish wake: %w", err)
	}
	if err := publish(Event{Type: "voice.capture_started", Mode: "assist"}); err != nil {
		return nil, fmt.Errorf("publish capture started: %w", err)
	}
	if err := publish(Event{Type: "voice.capture_stopped", Mode: "assist", ReasonCode: "fake_utterance_complete"}); err != nil {
		return nil, fmt.Errorf("publish capture stopped: %w", err)
	}

	assist, err := a.callAssist(ctx, AssistRequest{
		RequestID: requestID,
		SessionID: sessionID,
		CommandID: commandID,
		DeviceID:  a.cfg.Device.DeviceID,
		RoomID:    a.cfg.Device.RoomID,
		Text:      text,
		Locale:    locale,
	})
	if err != nil {
		return nil, fmt.Errorf("local Home Assistant bridge: %w", err)
	}
	if strings.TrimSpace(assist.Speech) == "" {
		return nil, errors.New("local Home Assistant bridge returned no speech")
	}
	if err := publish(Event{Type: "voice.assist_result", Mode: "assist", SpeakText: assist.Speech, ReasonCode: assist.ReasonCode}); err != nil {
		return nil, fmt.Errorf("publish assist result: %w", err)
	}

	if err := publish(Event{Type: "voice.tts_started", Mode: "assist"}); err != nil {
		return nil, fmt.Errorf("publish tts started: %w", err)
	}
	ttsResult, err := a.callSpeechKitTTS(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("speechkit local tts bridge: %w", err)
	}
	if err := publish(Event{
		Type:      "voice.tts_finished",
		Mode:      "assist",
		SpeakText: assist.Speech,
		Metadata:  map[string]string{"provider": ttsResult.Provider, "format": ttsResult.Format},
	}); err != nil {
		return nil, fmt.Errorf("publish tts finished: %w", err)
	}

	return &CycleResult{
		RequestID:      requestID,
		SessionID:      sessionID,
		SpokenText:     assist.Speech,
		ConversationID: assist.ConversationID,
		ResponseType:   assist.ResponseType,
		TTSProvider:    ttsResult.Provider,
		Replayed:       assist.Replayed,
		Events:         events,
	}, nil
}

func (a *Agent) Publish(ctx context.Context, ev Event) error {
	ev = a.fillEventDefaults(ev, ev.RequestID, ev.SessionID)
	var ack EventAck
	return a.postServerJSON(ctx, "/v1/device-agent/events", ev, &ack)
}

func (a *Agent) fillEventDefaults(ev Event, requestID, sessionID string) Event {
	ev.Surface = firstNonEmpty(ev.Surface, "device_agent")
	ev.Mode = firstNonEmpty(ev.Mode, "assist")
	ev.RequestID = firstNonEmpty(ev.RequestID, requestID)
	ev.SessionID = firstNonEmpty(ev.SessionID, sessionID)
	ev.DeviceID = firstNonEmpty(ev.DeviceID, a.cfg.Device.DeviceID)
	ev.RoomID = firstNonEmpty(ev.RoomID, a.cfg.Device.RoomID)
	ev.CapturePolicy = firstNonEmpty(ev.CapturePolicy, "device_agent")
	ev.Transport = "local_http"
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	return ev
}

func (a *Agent) callAssist(ctx context.Context, request AssistRequest) (*AssistResponse, error) {
	var out AssistResponse
	if err := a.postServerJSON(ctx, "/v1/device-agent/assist", request, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.RequestID) != strings.TrimSpace(request.RequestID) {
		return nil, fmt.Errorf("%w: expected request_id %q, got %q", ErrAssistResponseMismatch, request.RequestID, out.RequestID)
	}
	if out.Status != "success" && out.Status != "denied" {
		return nil, fmt.Errorf("%w: unsupported status %q", ErrAssistResponseMismatch, out.Status)
	}
	switch out.ActionExecuted {
	case "yes", "no", "not_applicable":
	default:
		return nil, fmt.Errorf("%w: unsupported action_executed %q", ErrAssistResponseMismatch, out.ActionExecuted)
	}
	return &out, nil
}

func (a *Agent) callSpeechKitTTS(ctx context.Context, requestID string) (*TTSResponse, error) {
	var out TTSResponse
	if err := a.postServerJSON(ctx, "/v1/device-agent/tts", TTSRequest{
		RequestID: requestID,
		Format:    "wav",
	}, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.RequestID) != strings.TrimSpace(requestID) {
		return nil, fmt.Errorf("%w: TTS request_id does not match", ErrAssistResponseMismatch)
	}
	return &out, nil
}

func (a *Agent) postServerJSON(ctx context.Context, path string, body, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolve(a.serverURL, path), &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", a.userAgent)
	req.Header.Set("Authorization", "Bearer "+a.cfg.PairingToken)
	req.Header.Set("X-SpeechKit-Device-ID", a.cfg.Device.DeviceID)
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // response body is fully read below
	responseInstanceID := strings.TrimSpace(resp.Header.Get(ServerInstanceHeader))
	if responseInstanceID == "" {
		return ErrServerIdentityMissing
	}
	if responseInstanceID != "" && responseInstanceID != a.cfg.ExpectedServerInstanceID {
		return fmt.Errorf("%w: expected %q, got %q", ErrServerIdentityMismatch, a.cfg.ExpectedServerInstanceID, responseInstanceID)
	}
	limit := int64(maxJSONResponseBytes)
	if path == "/v1/device-agent/tts" {
		limit = maxTTSResponseBytes
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limit = maxErrorResponseBytes
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > limit {
		return ErrResponseTooLarge
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		httpErr := &HTTPError{Method: http.MethodPost, Path: path, StatusCode: resp.StatusCode}
		_ = json.Unmarshal(raw, &httpErr.Envelope)
		return httpErr
	}
	if out != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode %s response: %w", path, err)
		}
	}
	return nil
}

func parseLocalBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return nil, errors.New("empty URL")
	}
	if err := netsec.ValidateProviderURL(raw, localValidation()); err != nil {
		return nil, err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("server URL must be an origin without path, query, or fragment")
	}
	if strings.EqualFold(u.Scheme, "http") && !localHTTPHostAllowed(u.Hostname()) {
		return nil, ErrInsecureServerTransport
	}
	return u, nil
}

func localHTTPHostAllowed(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

func validPairingToken(value string) bool {
	if len(value) < minimumPairingTokenBytes || len(value) > 512 {
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

func localValidation() netsec.ValidationOptions {
	return netsec.ValidationOptions{
		AllowLoopback: true,
		AllowPrivate:  true,
		AllowHTTP:     true,
		RequireLocal:  true,
	}
}

func resolve(base *url.URL, path string) string {
	ref := &url.URL{Path: path}
	return base.ResolveReference(ref).String()
}

func normalizeDevice(d DeviceDescriptor) DeviceDescriptor {
	d.AgentID = firstNonEmpty(d.AgentID, "speechkit-device-agent")
	d.DeviceID = firstNonEmpty(d.DeviceID, "speechkit-device-agent-001")
	d.DisplayName = firstNonEmpty(d.DisplayName, d.DeviceID)
	d.RoomID = firstNonEmpty(d.RoomID, "default")
	d.CaptureDevice.Kind = firstNonEmpty(d.CaptureDevice.Kind, "microphone")
	d.OutputDevice.Kind = firstNonEmpty(d.OutputDevice.Kind, "speaker")
	d.Wakeword.Status = firstNonEmpty(d.Wakeword.Status, CapabilityUnverified)
	return d
}

func normalizeHealth(h Health) Health {
	h.Status = firstNonEmpty(h.Status, CapabilityUnverified)
	return h
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
