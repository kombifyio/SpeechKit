package deviceagent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

const testAgentPairingToken = "pairing-token-0123456789abcdefghi"

func TestFakeAssistCycleUsesOnlyPairedLocalServer(t *testing.T) {
	var registrations []Registration
	var events []Event
	var assistRequests []AssistRequest
	var ttsRequests []TTSRequest

	mux := http.NewServeMux()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(ServerInstanceHeader, "homelab-1")
		mux.ServeHTTP(w, r)
	}))
	defer server.Close()

	authenticated := func(w http.ResponseWriter, r *http.Request) bool {
		t.Helper()
		if got := r.Header.Get("Authorization"); got != "Bearer "+testAgentPairingToken {
			t.Errorf("Authorization = %q", got)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return false
		}
		if got := r.Header.Get("X-SpeechKit-Device-ID"); got != "speaker-kitchen-001" {
			t.Errorf("device header = %q", got)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("/v1/device-agent/register", func(w http.ResponseWriter, r *http.Request) {
		if !authenticated(w, r) {
			return
		}
		var body Registration
		decodeTestJSON(t, r, &body)
		registrations = append(registrations, body)
		writeTestJSON(t, w, RegistrationAck{
			Status:           "paired",
			PairingID:        "pairing-kitchen",
			ServerInstanceID: "homelab-1",
			Capabilities: BridgeCapabilities{
				HomeAssistant: CapabilityState{Status: CapabilityReady},
				TTS:           CapabilityState{Status: CapabilityReady},
			},
		})
	})
	mux.HandleFunc("/v1/device-agent/events", func(w http.ResponseWriter, r *http.Request) {
		if !authenticated(w, r) {
			return
		}
		var body Event
		decodeTestJSON(t, r, &body)
		events = append(events, body)
		writeTestJSON(t, w, EventAck{Status: "accepted"})
	})
	mux.HandleFunc("/v1/device-agent/assist", func(w http.ResponseWriter, r *http.Request) {
		if !authenticated(w, r) {
			return
		}
		var body AssistRequest
		decodeTestJSON(t, r, &body)
		assistRequests = append(assistRequests, body)
		writeTestJSON(t, w, AssistResponse{
			Status:         "success",
			RequestID:      body.RequestID,
			ConversationID: "ha-conversation-1",
			ResponseType:   "action_done",
			Speech:         "Das Licht in der Kueche ist aus.",
			ActionExecuted: "yes",
		})
	})
	mux.HandleFunc("/v1/device-agent/tts", func(w http.ResponseWriter, r *http.Request) {
		if !authenticated(w, r) {
			return
		}
		var body TTSRequest
		decodeTestJSON(t, r, &body)
		ttsRequests = append(ttsRequests, body)
		writeTestJSON(t, w, TTSResponse{
			RequestID:   body.RequestID,
			AudioBase64: "UklGRg==",
			Format:      "wav",
			SampleRate:  16000,
			Provider:    "fake-piper",
		})
	})

	agent, err := New(Config{
		ServerURL:                server.URL,
		PairingToken:             testAgentPairingToken,
		ExpectedPairingID:        "pairing-kitchen",
		ExpectedServerInstanceID: "homelab-1",
		Device: DeviceDescriptor{
			AgentID:     "agent-kitchen",
			DeviceID:    "speaker-kitchen-001",
			DisplayName: "Kitchen speaker",
			RoomID:      "kitchen",
			CaptureDevice: AudioDevice{
				ID:        "fake-mic-1",
				Kind:      "microphone",
				Transport: "fake",
			},
			OutputDevice: AudioDevice{
				ID:        "fake-speaker-1",
				Kind:      "speaker",
				Transport: "fake",
			},
			Wakeword: Wakeword{Enabled: true, Phrase: "Hey Kombify", Backend: "fake", Status: CapabilityReady},
		},
		Capabilities: Capabilities{Assist: true, WakewordLocal: true, TTS: true},
		Health:       Health{Status: CapabilityReady, CaptureReady: true, OutputReady: true, WakeReady: true},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := agent.RunFakeAssistCycle(t.Context(), CycleOptions{
		CommandID: "kitchen-light-off",
		Text:      "mach das licht in der kueche aus",
		Locale:    "de-DE",
	})
	if err != nil {
		t.Fatalf("RunFakeAssistCycle: %v", err)
	}
	if _, err := uuid.Parse(result.RequestID); err != nil {
		t.Fatalf("request id = %q: %v", result.RequestID, err)
	}
	if result.SpokenText != "Das Licht in der Kueche ist aus." || result.TTSProvider != "fake-piper" {
		t.Fatalf("result = %#v", result)
	}
	if len(registrations) != 1 {
		t.Fatalf("registrations = %d", len(registrations))
	}
	reg := registrations[0]
	if reg.Version != CurrentProtocolVersion || reg.Device.RoomID != "kitchen" {
		t.Fatalf("registration = %#v", reg)
	}
	if reg.Capabilities.Dictation || reg.Capabilities.VoiceAgent || reg.Capabilities.BargeIn {
		t.Fatalf("registration invented capabilities: %#v", reg.Capabilities)
	}
	if !reg.Capabilities.Assist || !reg.Capabilities.WakewordLocal || !reg.Capabilities.TTS {
		t.Fatalf("registration omitted reported fake capabilities: %#v", reg.Capabilities)
	}
	if len(assistRequests) != 1 {
		t.Fatalf("assist requests = %d", len(assistRequests))
	}
	assist := assistRequests[0]
	if assist.DeviceID != "speaker-kitchen-001" || assist.RoomID != "kitchen" || assist.CommandID != "kitchen-light-off" || assist.Text == "" {
		t.Fatalf("assist request = %#v", assist)
	}
	rawAssist, _ := json.Marshal(assist)
	for _, forbidden := range []string{"home_assistant", "token", "target_url", "provider"} {
		if strings.Contains(strings.ToLower(string(rawAssist)), forbidden) {
			t.Fatalf("agent-to-server request contains forbidden field %q: %s", forbidden, rawAssist)
		}
	}
	if len(ttsRequests) != 1 || ttsRequests[0].RequestID != result.RequestID {
		t.Fatalf("tts requests = %#v", ttsRequests)
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
		if event.Transport != "local_http" || event.DeviceID != "speaker-kitchen-001" || event.RoomID != "kitchen" {
			t.Fatalf("event boundary fields = %#v", event)
		}
	}
}

func TestNewFailsClosedForMissingBindingAndPublicServer(t *testing.T) {
	base := Config{
		ServerURL: "http://127.0.0.1:8080",
	}
	cases := []struct {
		name string
		cfg  Config
		want error
	}{
		{name: "missing server", cfg: Config{PairingToken: testAgentPairingToken, ExpectedServerInstanceID: "x", ExpectedPairingID: "x"}, want: ErrMissingServerURL},
		{name: "missing pairing token", cfg: Config{ServerURL: base.ServerURL, ExpectedServerInstanceID: "x", ExpectedPairingID: "x"}, want: ErrMissingPairingToken},
		{name: "missing expected identity", cfg: Config{ServerURL: base.ServerURL, PairingToken: testAgentPairingToken, ExpectedPairingID: "x"}, want: ErrMissingExpectedServerInstance},
		{name: "missing pairing identity", cfg: Config{ServerURL: base.ServerURL, PairingToken: testAgentPairingToken, ExpectedServerInstanceID: "x"}, want: ErrMissingExpectedPairingID},
		{name: "short pairing token", cfg: Config{ServerURL: base.ServerURL, PairingToken: "short", ExpectedServerInstanceID: "x", ExpectedPairingID: "x"}, want: ErrPairingTokenTooShort},
		{name: "public literal", cfg: Config{ServerURL: "https://8.8.8.8", PairingToken: testAgentPairingToken, ExpectedServerInstanceID: "x", ExpectedPairingID: "x"}, want: netsec.ErrPublicBlocked},
		{name: "plaintext LAN", cfg: Config{ServerURL: "http://192.168.1.10:8080", PairingToken: testAgentPairingToken, ExpectedServerInstanceID: "x", ExpectedPairingID: "x"}, want: ErrInsecureServerTransport},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.cfg)
			if !errors.Is(err, tc.want) {
				t.Fatalf("New error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewRejectsDeprecatedCredentialAndTransportConfiguration(t *testing.T) {
	base := Config{
		ServerURL:                "http://127.0.0.1:8080",
		PairingToken:             testAgentPairingToken,
		ExpectedServerInstanceID: "homelab-1",
		ExpectedPairingID:        "pairing-kitchen",
	}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "server token", mutate: func(cfg *Config) { cfg.ServerToken = "legacy-server-token" }},
		{name: "HA URL", mutate: func(cfg *Config) { cfg.HomeAssistantURL = "http://127.0.0.1:8123" }},
		{name: "HA token", mutate: func(cfg *Config) { cfg.HomeAssistantToken = "legacy-ha-token" }},
		{name: "HA agent", mutate: func(cfg *Config) { cfg.HomeAssistantAgent = "conversation.home_assistant" }},
		{name: "custom HTTP client", mutate: func(cfg *Config) { cfg.HTTPClient = &http.Client{} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			if _, err := New(cfg); !errors.Is(err, ErrLegacyClientConfig) {
				t.Fatalf("New error = %v, want %v", err, ErrLegacyClientConfig)
			}
		})
	}
}

func TestDeprecatedAuthorityFieldsNeverEnterTheWireContract(t *testing.T) {
	registration, err := json.Marshal(Registration{
		Version: CurrentProtocolVersion,
		Capabilities: Capabilities{
			Assist:       true,
			LocalPairing: true,
		},
		Pairing: Pairing{Status: "paired", Method: "legacy-device-assertion"},
	})
	if err != nil {
		t.Fatalf("marshal registration: %v", err)
	}
	for _, forbidden := range []string{"local_pairing", "legacy-device-assertion", `"pairing"`} {
		if strings.Contains(string(registration), forbidden) {
			t.Fatalf("registration leaked deprecated field %q: %s", forbidden, registration)
		}
	}

	result, err := json.Marshal(CycleResult{HomeAssistantRaw: "legacy-secret-bearing-response"})
	if err != nil {
		t.Fatalf("marshal cycle result: %v", err)
	}
	if strings.Contains(string(result), "legacy-secret-bearing-response") || strings.Contains(string(result), "home_assistant_raw") {
		t.Fatalf("cycle result leaked deprecated Home Assistant payload: %s", result)
	}
}

func TestRegisterRejectsChangedServerIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(ServerInstanceHeader, "different-server")
		writeTestJSON(t, w, RegistrationAck{Status: "paired", ServerInstanceID: "different-server"})
	}))
	defer server.Close()
	agent, err := New(Config{
		ServerURL:                server.URL,
		PairingToken:             testAgentPairingToken,
		ExpectedPairingID:        "pairing-kitchen",
		ExpectedServerInstanceID: "homelab-1",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = agent.Register(t.Context())
	if !errors.Is(err, ErrServerIdentityMismatch) {
		t.Fatalf("Register error = %v", err)
	}
}

func TestRegisterRejectsMissingServerIdentityHeaderAndChangedPairingEpoch(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		pairing string
		want    error
	}{
		{name: "missing server identity header", pairing: "pairing-kitchen", want: ErrServerIdentityMissing},
		{name: "changed pairing epoch", header: "homelab-1", pairing: "pairing-kitchen-v2", want: ErrPairingIdentityMismatch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.header != "" {
					w.Header().Set(ServerInstanceHeader, tc.header)
				}
				writeTestJSON(t, w, RegistrationAck{Status: "paired", PairingID: tc.pairing, ServerInstanceID: "homelab-1"})
			}))
			defer server.Close()
			agent, err := New(Config{
				ServerURL: server.URL, PairingToken: testAgentPairingToken,
				ExpectedServerInstanceID: "homelab-1", ExpectedPairingID: "pairing-kitchen",
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = agent.Register(t.Context())
			if !errors.Is(err, tc.want) {
				t.Fatalf("Register error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAssistResponseMustCorrelateAndUseTerminalSemantics(t *testing.T) {
	tests := []struct {
		name   string
		result AssistResponse
	}{
		{name: "wrong request", result: AssistResponse{RequestID: "other", Status: "success", ActionExecuted: "yes"}},
		{name: "unknown status", result: AssistResponse{RequestID: "request-1", Status: "maybe", ActionExecuted: "yes"}},
		{name: "unknown execution", result: AssistResponse{RequestID: "request-1", Status: "success", ActionExecuted: "unknown"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(ServerInstanceHeader, "homelab-1")
				writeTestJSON(t, w, tc.result)
			}))
			defer server.Close()
			agent, err := New(Config{
				ServerURL: server.URL, PairingToken: testAgentPairingToken,
				ExpectedServerInstanceID: "homelab-1", ExpectedPairingID: "pairing-kitchen",
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = agent.callAssist(t.Context(), AssistRequest{RequestID: "request-1"})
			if !errors.Is(err, ErrAssistResponseMismatch) {
				t.Fatalf("callAssist error = %v, want %v", err, ErrAssistResponseMismatch)
			}
		})
	}
}

func TestUnstructuredHTTPErrorDoesNotEchoBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(ServerInstanceHeader, "homelab-1")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("sensitive reflected request text"))
	}))
	defer server.Close()
	agent, err := New(Config{
		ServerURL: server.URL, PairingToken: testAgentPairingToken,
		ExpectedServerInstanceID: "homelab-1", ExpectedPairingID: "pairing-kitchen",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = agent.callAssist(t.Context(), AssistRequest{RequestID: "request-1"})
	if err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("error leaked response body: %v", err)
	}
}

func TestNewRejectsUnsafePairingCredentialBeforeNetwork(t *testing.T) {
	for _, token := range []string{
		"pairing-token-0123456789abcdef\r\nInjected: value",
		strings.Repeat("x", 513),
	} {
		_, err := New(Config{
			ServerURL: "http://localhost:8080", PairingToken: token,
			ExpectedServerInstanceID: "homelab-1", ExpectedPairingID: "pairing-kitchen",
		})
		if !errors.Is(err, ErrPairingTokenInvalid) {
			t.Fatalf("New token length=%d error=%v, want %v", len(token), err, ErrPairingTokenInvalid)
		}
	}
}

func TestHTTPErrorPreservesStableReasonEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(ServerInstanceHeader, "homelab-1")
		w.WriteHeader(http.StatusConflict)
		writeTestJSON(t, w, ErrorEnvelope{Error: BridgeError{
			ErrorCode:      "request_conflict",
			ReasonCode:     "request_digest_mismatch",
			ActionExecuted: "no",
			UserGuidance:   "Use a new UUIDv7 request_id.",
		}})
	}))
	defer server.Close()
	agent, err := New(Config{
		ServerURL:                server.URL,
		PairingToken:             testAgentPairingToken,
		ExpectedPairingID:        "pairing-kitchen",
		ExpectedServerInstanceID: "homelab-1",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = agent.callAssist(t.Context(), AssistRequest{RequestID: "x"})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if httpErr.StatusCode != http.StatusConflict || httpErr.Envelope.Error.ReasonCode != "request_digest_mismatch" {
		t.Fatalf("HTTP error = %#v", httpErr)
	}
}

func TestTTSResponseMustCorrelateRequestID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(ServerInstanceHeader, "homelab-1")
		writeTestJSON(t, w, TTSResponse{
			RequestID: "different-request", AudioBase64: "UklGRg==", Format: "wav",
			SampleRate: 16000, Provider: "fake-piper",
		})
	}))
	defer server.Close()
	agent, err := New(Config{
		ServerURL: server.URL, PairingToken: testAgentPairingToken,
		ExpectedPairingID: "pairing-kitchen", ExpectedServerInstanceID: "homelab-1",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = agent.callSpeechKitTTS(t.Context(), "expected-request")
	if !errors.Is(err, ErrAssistResponseMismatch) {
		t.Fatalf("callSpeechKitTTS error = %v, want %v", err, ErrAssistResponseMismatch)
	}
}

func assertEventOrder(t *testing.T, events []Event, want []string) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("events = %d, want %d: %#v", len(events), len(want), events)
	}
	for i := range want {
		if events[i].Type != want[i] {
			t.Fatalf("event[%d] = %q, want %q", i, events[i].Type, want[i])
		}
	}
}

func decodeTestJSON(t *testing.T, r *http.Request, out any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		t.Fatalf("decode request: %v", err)
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
