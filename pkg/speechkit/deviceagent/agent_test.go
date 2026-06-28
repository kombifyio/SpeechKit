package deviceagent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFakeHomelabAssistCyclePublishesWakeCaptureHAAndTTS(t *testing.T) {
	var registrations []Registration
	var events []Event
	var ttsRequests []map[string]any

	speechkitMux := http.NewServeMux()
	speechkitServer := httptest.NewServer(speechkitMux)
	defer speechkitServer.Close()
	speechkitMux.HandleFunc("/v1/device-agent/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("register method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer local-server-token" {
			t.Fatalf("register Authorization = %q", got)
		}
		var reg Registration
		if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
			t.Fatalf("decode registration: %v", err)
		}
		registrations = append(registrations, reg)
		_ = json.NewEncoder(w).Encode(RegistrationAck{Status: "paired", PairingID: "pairing-1"})
	})
	speechkitMux.HandleFunc("/v1/device-agent/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("event method = %s, want POST", r.Method)
		}
		var event Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		events = append(events, event)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	})
	speechkitMux.HandleFunc("/v1/tts/synthesize", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode tts request: %v", err)
		}
		ttsRequests = append(ttsRequests, body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"audio_base64": "UklGRg==",
			"format":       "wav",
			"provider":     "fake-piper",
		})
	})

	var haCalled bool
	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/conversation/process" {
			t.Fatalf("Home Assistant path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer local-ha-token" {
			t.Fatalf("Home Assistant Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode HA request: %v", err)
		}
		if body["text"] != "mach das licht in der kueche aus" {
			t.Fatalf("HA text = %#v", body["text"])
		}
		if body["agent_id"] != "conversation.home_assistant" {
			t.Fatalf("HA agent_id = %#v", body["agent_id"])
		}
		haCalled = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"conversation_id": "ha-conversation-1",
			"response": map[string]any{
				"speech": map[string]any{
					"plain": map[string]any{"speech": "Das Licht in der Kueche ist aus."},
				},
			},
		})
	}))
	defer haServer.Close()

	agent, err := New(Config{
		ServerURL:          speechkitServer.URL,
		ServerToken:        "local-server-token",
		HomeAssistantURL:   haServer.URL,
		HomeAssistantToken: "local-ha-token",
		HomeAssistantAgent: "conversation.home_assistant",
		Device: DeviceDescriptor{
			AgentID:     "agent-kitchen",
			DeviceID:    "speaker-kitchen-001",
			DisplayName: "Kitchen speaker",
			RoomID:      "kitchen",
			CaptureDevice: AudioDevice{
				ID:        "fake-mic-1",
				Name:      "Fake kitchen mic",
				Kind:      "microphone",
				Transport: "fake",
			},
			OutputDevice: AudioDevice{
				ID:        "fake-speaker-1",
				Name:      "Fake kitchen speaker",
				Kind:      "speaker",
				Transport: "fake",
			},
			Wakeword: Wakeword{Enabled: true, Phrase: "Hey Kombify", Backend: "fake", Status: "ready"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	started := time.Now()
	result, err := agent.RunFakeAssistCycle(t.Context(), CycleOptions{
		SessionID: "assist-session-001",
		Text:      "mach das licht in der kueche aus",
		Locale:    "de-DE",
	})
	if err != nil {
		t.Fatalf("RunFakeAssistCycle: %v", err)
	}
	if time.Since(started) > 15*time.Minute {
		t.Fatal("fake homelab cycle exceeded 15-minute policy")
	}

	if len(registrations) != 1 {
		t.Fatalf("registrations = %d, want 1", len(registrations))
	}
	reg := registrations[0]
	if reg.Version != ProtocolVersion {
		t.Fatalf("registration version = %q", reg.Version)
	}
	if reg.Device.RoomID != "kitchen" || reg.Device.CaptureDevice.ID != "fake-mic-1" || reg.Device.OutputDevice.ID != "fake-speaker-1" {
		t.Fatalf("registration device = %#v", reg.Device)
	}
	if !reg.Capabilities.WakewordLocal || !reg.Capabilities.LocalPairing {
		t.Fatalf("registration capabilities = %#v", reg.Capabilities)
	}
	if !haCalled {
		t.Fatal("Home Assistant conversation endpoint was not called")
	}
	if len(ttsRequests) != 1 {
		t.Fatalf("tts requests = %d, want 1", len(ttsRequests))
	}
	if got := ttsRequests[0]["text"]; got != "Das Licht in der Kueche ist aus." {
		t.Fatalf("tts text = %#v", got)
	}
	if result.SpokenText != "Das Licht in der Kueche ist aus." || result.TTSProvider != "fake-piper" {
		t.Fatalf("result = %#v", result)
	}
	assertEventOrder(t, events, []string{
		"device.wake_detected",
		"voice.capture_started",
		"voice.capture_stopped",
		"voice.assist_result",
		"voice.tts_started",
		"voice.tts_finished",
	})
	for _, event := range events {
		if event.Surface != "device_agent" || event.CapturePolicy != "device_agent" || event.Transport != "local_http" {
			t.Fatalf("event defaults not set: %#v", event)
		}
		if event.DeviceID != "speaker-kitchen-001" || event.RoomID != "kitchen" {
			t.Fatalf("event device routing not set: %#v", event)
		}
	}
}

func TestRequiresLocalHomeAssistantToken(t *testing.T) {
	agent, err := New(Config{ServerURL: "http://127.0.0.1:8124", HomeAssistantURL: "http://127.0.0.1:8123"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = agent.callHomeAssistant(t.Context(), "licht aus", "de-DE")
	if err == nil || !strings.Contains(err.Error(), ErrMissingHomeAssistantToken.Error()) {
		t.Fatalf("RunFakeAssistCycle error = %v, want missing HA token", err)
	}
}

func assertEventOrder(t *testing.T, events []Event, want []string) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("events = %d, want %d: %#v", len(events), len(want), events)
	}
	for i, event := range events {
		if event.Type != want[i] {
			t.Fatalf("event[%d] = %q, want %q; events=%#v", i, event.Type, want[i], events)
		}
	}
}
