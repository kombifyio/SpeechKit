// Package deviceagent implements the LAN-side SpeechKit device agent contract.
package deviceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ProtocolVersion  = "speechkit.device_agent.v0"
	defaultLocale    = "de-DE"
	defaultUserAgent = "speechkit-device-agent/0.1"
)

var (
	ErrMissingServerURL          = errors.New("speechkit deviceagent: server_url is required")
	ErrMissingHomeAssistantURL   = errors.New("speechkit deviceagent: home_assistant_url is required")
	ErrMissingHomeAssistantToken = errors.New("speechkit deviceagent: home_assistant_token is required")
	ErrMissingAssistText         = errors.New("speechkit deviceagent: assist text is required")
)

type Config struct {
	ServerURL          string
	ServerToken        string
	HomeAssistantURL   string
	HomeAssistantToken string
	HomeAssistantAgent string
	HTTPClient         *http.Client
	UserAgent          string
	Device             DeviceDescriptor
	Locale             string
}

type DeviceDescriptor struct {
	AgentID       string      `json:"agent_id"`
	DeviceID      string      `json:"device_id"`
	DisplayName   string      `json:"display_name,omitempty"`
	RoomID        string      `json:"room_id,omitempty"`
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

type Registration struct {
	Version      string           `json:"version"`
	RegisteredAt time.Time        `json:"registered_at"`
	Device       DeviceDescriptor `json:"device"`
	Capabilities Capabilities     `json:"capabilities"`
	Health       Health           `json:"health"`
	Pairing      Pairing          `json:"pairing"`
}

type RegistrationAck struct {
	Status     string `json:"status"`
	PairingID  string `json:"pairing_id,omitempty"`
	ServerTime string `json:"server_time,omitempty"`
}

type Capabilities struct {
	Dictation     bool `json:"dictation"`
	Assist        bool `json:"assist"`
	VoiceAgent    bool `json:"voice_agent"`
	WakewordLocal bool `json:"wakeword_local"`
	TTS           bool `json:"tts"`
	BargeIn       bool `json:"barge_in"`
	LocalPairing  bool `json:"local_pairing"`
}

type Health struct {
	Status       string `json:"status"`
	CaptureReady bool   `json:"capture_ready"`
	OutputReady  bool   `json:"output_ready"`
	WakeReady    bool   `json:"wake_ready"`
}

type Pairing struct {
	Status string `json:"status"`
	Method string `json:"method"`
}

type Event struct {
	Type          string            `json:"type"`
	Surface       string            `json:"surface"`
	Mode          string            `json:"mode"`
	SessionID     string            `json:"session_id,omitempty"`
	DeviceID      string            `json:"device_id"`
	RoomID        string            `json:"room_id,omitempty"`
	CapturePolicy string            `json:"capture_policy,omitempty"`
	Transport     string            `json:"transport,omitempty"`
	Text          string            `json:"text,omitempty"`
	SpeakText     string            `json:"speak_text,omitempty"`
	ReasonCode    string            `json:"reason_code,omitempty"`
	At            time.Time         `json:"at"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type CycleOptions struct {
	SessionID string
	Text      string
	Locale    string
}

type CycleResult struct {
	SessionID        string  `json:"session_id"`
	SpokenText       string  `json:"spoken_text"`
	HomeAssistantRaw string  `json:"home_assistant_raw,omitempty"`
	TTSProvider      string  `json:"tts_provider,omitempty"`
	Events           []Event `json:"events"`
}

type Agent struct {
	cfg       Config
	serverURL *url.URL
	haURL     *url.URL
	http      *http.Client
	userAgent string
}

func New(cfg Config) (*Agent, error) {
	serverURL, err := parseBaseURL(cfg.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMissingServerURL, err)
	}
	var haURL *url.URL
	if strings.TrimSpace(cfg.HomeAssistantURL) != "" {
		haURL, err = parseBaseURL(cfg.HomeAssistantURL)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMissingHomeAssistantURL, err)
		}
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	userAgent := strings.TrimSpace(cfg.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	cfg.Locale = firstNonEmpty(cfg.Locale, defaultLocale)
	cfg.Device = normalizeDevice(cfg.Device)
	return &Agent{cfg: cfg, serverURL: serverURL, haURL: haURL, http: httpClient, userAgent: userAgent}, nil
}

func (a *Agent) Register(ctx context.Context) (*RegistrationAck, error) {
	reg := Registration{
		Version:      ProtocolVersion,
		RegisteredAt: time.Now().UTC(),
		Device:       a.cfg.Device,
		Capabilities: Capabilities{
			Dictation:     true,
			Assist:        true,
			VoiceAgent:    true,
			WakewordLocal: true,
			TTS:           true,
			BargeIn:       true,
			LocalPairing:  true,
		},
		Health: Health{
			Status:       "ok",
			CaptureReady: true,
			OutputReady:  true,
			WakeReady:    a.cfg.Device.Wakeword.Enabled,
		},
		Pairing: Pairing{Status: "paired", Method: "local_lan"},
	}
	var ack RegistrationAck
	if err := a.postServerJSON(ctx, "/v1/device-agent/register", reg, &ack); err != nil {
		return nil, err
	}
	return &ack, nil
}

func (a *Agent) RunFakeAssistCycle(ctx context.Context, opts CycleOptions) (*CycleResult, error) {
	text := strings.TrimSpace(opts.Text)
	if text == "" {
		return nil, ErrMissingAssistText
	}
	sessionID := firstNonEmpty(opts.SessionID, fmt.Sprintf("device-agent-%d", time.Now().UnixNano()))
	locale := firstNonEmpty(opts.Locale, a.cfg.Locale, defaultLocale)
	events := []Event{}

	if _, err := a.Register(ctx); err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}
	publish := func(ev Event) error {
		ev = a.fillEventDefaults(ev, sessionID)
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
	if err := publish(Event{Type: "voice.capture_stopped", Mode: "assist", ReasonCode: "fake_utterance_complete", Text: text}); err != nil {
		return nil, fmt.Errorf("publish capture stopped: %w", err)
	}

	ha, err := a.callHomeAssistant(ctx, text, locale)
	if err != nil {
		return nil, fmt.Errorf("home assistant conversation: %w", err)
	}
	if err := publish(Event{Type: "voice.assist_result", Mode: "assist", Text: ha.Speech, SpeakText: ha.Speech}); err != nil {
		return nil, fmt.Errorf("publish assist result: %w", err)
	}

	if err := publish(Event{Type: "voice.tts_started", Mode: "assist", Text: ha.Speech}); err != nil {
		return nil, fmt.Errorf("publish tts started: %w", err)
	}
	tts, err := a.callSpeechKitTTS(ctx, ha.Speech, locale)
	if err != nil {
		return nil, fmt.Errorf("speechkit tts: %w", err)
	}
	if err := publish(Event{
		Type:      "voice.tts_finished",
		Mode:      "assist",
		Text:      ha.Speech,
		SpeakText: ha.Speech,
		Metadata:  map[string]string{"provider": tts.Provider, "format": tts.Format},
	}); err != nil {
		return nil, fmt.Errorf("publish tts finished: %w", err)
	}

	return &CycleResult{
		SessionID:        sessionID,
		SpokenText:       ha.Speech,
		HomeAssistantRaw: ha.Raw,
		TTSProvider:      tts.Provider,
		Events:           events,
	}, nil
}

func (a *Agent) Publish(ctx context.Context, ev Event) error {
	ev = a.fillEventDefaults(ev, ev.SessionID)
	return a.postServerJSON(ctx, "/v1/device-agent/events", ev, nil)
}

func (a *Agent) fillEventDefaults(ev Event, sessionID string) Event {
	ev.Surface = firstNonEmpty(ev.Surface, "device_agent")
	ev.Mode = firstNonEmpty(ev.Mode, "assist")
	ev.SessionID = firstNonEmpty(ev.SessionID, sessionID)
	ev.DeviceID = firstNonEmpty(ev.DeviceID, a.cfg.Device.DeviceID)
	ev.RoomID = firstNonEmpty(ev.RoomID, a.cfg.Device.RoomID)
	ev.CapturePolicy = firstNonEmpty(ev.CapturePolicy, "device_agent")
	ev.Transport = firstNonEmpty(ev.Transport, "local_http")
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	return ev
}

type homeAssistantConversation struct {
	Speech string
	Raw    string
}

func (a *Agent) callHomeAssistant(ctx context.Context, text, locale string) (*homeAssistantConversation, error) {
	if a.haURL == nil {
		return nil, ErrMissingHomeAssistantURL
	}
	if strings.TrimSpace(a.cfg.HomeAssistantToken) == "" {
		return nil, ErrMissingHomeAssistantToken
	}
	body := map[string]any{
		"text":     text,
		"language": locale,
	}
	if agent := strings.TrimSpace(a.cfg.HomeAssistantAgent); agent != "" {
		body["agent_id"] = agent
	}
	var out struct {
		ConversationID string `json:"conversation_id"`
		Response       struct {
			Speech struct {
				Plain *struct {
					Speech string `json:"speech"`
				} `json:"plain"`
				SSML *struct {
					Speech string `json:"speech"`
				} `json:"ssml"`
			} `json:"speech"`
		} `json:"response"`
	}
	raw, err := a.postHomeAssistantJSON(ctx, "/api/conversation/process", body, &out)
	if err != nil {
		return nil, err
	}
	speech := ""
	if out.Response.Speech.Plain != nil {
		speech = strings.TrimSpace(out.Response.Speech.Plain.Speech)
	}
	if speech == "" && out.Response.Speech.SSML != nil {
		speech = strings.TrimSpace(out.Response.Speech.SSML.Speech)
	}
	if speech == "" {
		return nil, errors.New("home assistant response did not include speech")
	}
	return &homeAssistantConversation{Speech: speech, Raw: string(raw)}, nil
}

type ttsResult struct {
	Provider string `json:"provider"`
	Format   string `json:"format"`
}

func (a *Agent) callSpeechKitTTS(ctx context.Context, text, locale string) (*ttsResult, error) {
	var out ttsResult
	if err := a.postServerJSON(ctx, "/v1/tts/synthesize", map[string]any{
		"text":   text,
		"locale": locale,
		"format": "wav",
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a *Agent) postServerJSON(ctx context.Context, path string, body, out any) error {
	_, err := a.doJSON(ctx, a.serverURL, path, a.cfg.ServerToken, body, out)
	return err
}

func (a *Agent) postHomeAssistantJSON(ctx context.Context, path string, body, out any) ([]byte, error) {
	if a.haURL == nil {
		return nil, ErrMissingHomeAssistantURL
	}
	return a.doJSON(ctx, a.haURL, path, a.cfg.HomeAssistantToken, body, out)
}

func (a *Agent) doJSON(ctx context.Context, base *url.URL, path, token string, body, out any) ([]byte, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolve(base, path), &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", a.userAgent)
	if token = strings.TrimSpace(token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // response body is fully read below
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return raw, fmt.Errorf("POST %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return raw, fmt.Errorf("decode %s response: %w", path, err)
		}
	}
	return raw, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return nil, errors.New("empty URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, errors.New("URL must include scheme and host")
	}
	return u, nil
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
	d.CaptureDevice.ID = firstNonEmpty(d.CaptureDevice.ID, "fake-mic")
	d.CaptureDevice.Name = firstNonEmpty(d.CaptureDevice.Name, "Fake microphone")
	d.CaptureDevice.Kind = firstNonEmpty(d.CaptureDevice.Kind, "microphone")
	d.CaptureDevice.Transport = firstNonEmpty(d.CaptureDevice.Transport, "fake")
	d.OutputDevice.ID = firstNonEmpty(d.OutputDevice.ID, "fake-speaker")
	d.OutputDevice.Name = firstNonEmpty(d.OutputDevice.Name, "Fake speaker")
	d.OutputDevice.Kind = firstNonEmpty(d.OutputDevice.Kind, "speaker")
	d.OutputDevice.Transport = firstNonEmpty(d.OutputDevice.Transport, "fake")
	d.Wakeword.Enabled = true
	d.Wakeword.Phrase = firstNonEmpty(d.Wakeword.Phrase, "Hey Kombify")
	d.Wakeword.Backend = firstNonEmpty(d.Wakeword.Backend, "fake")
	d.Wakeword.Status = firstNonEmpty(d.Wakeword.Status, "ready")
	return d
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
