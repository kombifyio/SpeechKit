// Package deviceagent implements the credential-minimal LAN-side SpeechKit
// device-agent client and its versioned wire contract.
package deviceagent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
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

// CycleOptions parameterizes one [Agent.RunFakeAssistCycle] run. Text and
// CommandID are required. RequestID defaults to a fresh UUIDv7, SessionID
// to "device-agent-" followed by the request ID, and Locale to the agent's
// configured locale.
type CycleOptions struct {
	RequestID string
	SessionID string
	CommandID string
	Text      string
	Locale    string
}

// CycleResult summarizes a completed fake Assist cycle: the correlation
// IDs, the text that was spoken, Home Assistant's conversation details, the
// TTS provider used, and every [Event] published, in order.
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

// Agent is a v1 device-agent client bound to one local speechkit-server.
// Every request carries the pairing token as a bearer credential and is
// verified against the expected server instance and pairing identities.
// Create it with [New]; an Agent is safe for concurrent use.
type Agent struct {
	cfg       Config
	serverURL *url.URL
	http      *http.Client
	userAgent string
}

// New validates cfg and returns an [Agent] whose HTTP client dials only
// local addresses, never follows redirects, and times out after 10 seconds.
// It returns [ErrMissingServerURL], [ErrMissingPairingToken],
// [ErrMissingExpectedServerInstance], [ErrMissingExpectedPairingID],
// [ErrPairingTokenTooShort], or [ErrPairingTokenInvalid] when cfg is
// incomplete or unsafe, and fills empty UserAgent, Locale, device, and
// health fields with defaults.
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

// Register performs the v1 handshake by posting a [Registration] built from
// the agent's configuration. It returns the ack only when the server proves
// its identity, reports Status "paired", and echoes the expected pairing
// ID; otherwise it fails closed with [ErrServerIdentityMismatch],
// [ErrPairingIdentityMismatch], or a descriptive error.
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

// RunFakeAssistCycle drives one deterministic wake → capture → Assist → TTS
// cycle without touching real audio: it registers, requires both server-side
// bridges to be [CapabilityReady], publishes the six lifecycle events, runs
// the command through the local Home Assistant bridge, and fetches the
// synthesized speech. It returns [ErrMissingAssistText] or
// [ErrMissingCommandID] before any network call,
// [ErrHomeAssistantBridgeUnavailable] or [ErrTTSBridgeUnavailable] when a
// bridge is not ready, and wraps every other failure with the failing step.
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

// Publish posts one lifecycle [Event] to the server. Empty Surface, Mode,
// DeviceID, RoomID, CapturePolicy, and At are filled with the agent's
// defaults and Transport is always "local_http"; RequestID and SessionID
// are sent as given.
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
