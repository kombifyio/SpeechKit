package deviceagent

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/deviceagent"
)

func TestBridgeProductionHTTPFlowIsPairedAtMostOnceAndTerminal(t *testing.T) {
	ledger := newFakeLedger()
	ha := &fakeHA{
		result: &HomeAssistantResult{
			ConversationID: "ha-conversation-1",
			ResponseType:   "action_done",
			Speech:         "Das Licht ist aus.",
			SuccessTargets: []HomeAssistantTarget{{Type: "entity", ID: "light.kitchen", Name: "Kitchen light"}},
			ActionExecuted: "unknown",
		},
	}
	bridge := newTestBridge(t, ha, ledger)
	mux := http.NewServeMux()
	bridge.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	registration := wire.Registration{
		Version: wire.CurrentProtocolVersion,
		Device: wire.DeviceDescriptor{
			DeviceID: "speaker-kitchen-001",
			RoomID:   "kitchen",
		},
		Capabilities: wire.Capabilities{Assist: true, WakewordLocal: true, TTS: true},
		Health:       wire.Health{Status: wire.CapabilityReady, CaptureReady: true, OutputReady: true, WakeReady: true},
	}
	registerResponse := postBridge(t, server.URL+"/v1/device-agent/register", registration, testPairingToken, "speaker-kitchen-001")
	if registerResponse.Code != http.StatusOK {
		t.Fatalf("register = %d %s", registerResponse.Code, registerResponse.Body.String())
	}
	var ack wire.RegistrationAck
	decodeRecorder(t, registerResponse, &ack)
	if ack.ServerInstanceID != "homelab-1" || ack.PairingID != "pairing-kitchen-v1" || ack.Capabilities.HomeAssistant.Status != wire.CapabilityReady || ack.Capabilities.TTS.Status != wire.CapabilityReady {
		t.Fatalf("ack = %#v", ack)
	}
	if _, err := time.Parse(time.RFC3339Nano, ack.ServerTime); err != nil {
		t.Fatalf("server_time = %q: %v", ack.ServerTime, err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	request := wire.AssistRequest{
		RequestID: id.String(),
		SessionID: "session-1",
		CommandID: "kitchen-light-off-de",
		DeviceID:  "speaker-kitchen-001",
		RoomID:    "kitchen",
		Text:      "mach das licht aus",
		Locale:    "de-DE",
	}
	first := postBridge(t, server.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
	if first.Code != http.StatusOK {
		t.Fatalf("first assist = %d %s", first.Code, first.Body.String())
	}
	var firstResult wire.AssistResponse
	decodeRecorder(t, first, &firstResult)
	if firstResult.Replayed || firstResult.ActionExecuted != "yes" || firstResult.Speech != "Das Licht ist aus." {
		t.Fatalf("first result = %#v", firstResult)
	}

	replay := postBridge(t, server.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay assist = %d %s", replay.Code, replay.Body.String())
	}
	var replayResult wire.AssistResponse
	decodeRecorder(t, replay, &replayResult)
	if !replayResult.Replayed || replayResult.Speech != firstResult.Speech || replayResult.ConversationID != firstResult.ConversationID {
		t.Fatalf("replay result = %#v", replayResult)
	}
	if ha.converseCalls != 1 {
		t.Fatalf("HA calls = %d, want 1", ha.converseCalls)
	}
	if ha.verifyCalls != 1 || ha.verifyEntity != "light.kitchen" || ha.verifyState != "off" {
		t.Fatalf("HA state verification = calls %d entity %q state %q", ha.verifyCalls, ha.verifyEntity, ha.verifyState)
	}

	request.CommandID = "kitchen-light-on-de"
	request.Text = "mach das licht an"
	conflict := postBridge(t, server.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
	assertBridgeError(t, conflict, http.StatusConflict, "request_digest_mismatch", "no")
	if ha.converseCalls != 1 {
		t.Fatalf("HA calls after conflict = %d", ha.converseCalls)
	}

	ttsResponse := postBridge(t, server.URL+"/v1/device-agent/tts", wire.TTSRequest{RequestID: firstResult.RequestID, Format: "wav"}, testPairingToken, request.DeviceID)
	if ttsResponse.Code != http.StatusOK {
		t.Fatalf("tts = %d %s", ttsResponse.Code, ttsResponse.Body.String())
	}
	var spoken wire.TTSResponse
	decodeRecorder(t, ttsResponse, &spoken)
	if spoken.RequestID != firstResult.RequestID || spoken.Provider != "fake-piper" || spoken.Format != "wav" || spoken.AudioBase64 == "" {
		t.Fatalf("tts response = %#v", spoken)
	}
}

func TestBridgeTTSRequiresCompletedClaimAndUsesOnlyPersistedSpeech(t *testing.T) {
	ledger := newFakeLedger()
	ha := &fakeHA{result: &HomeAssistantResult{
		ResponseType: "action_done", Speech: "The kitchen light is off.", ActionExecuted: "unknown",
		SuccessTargets: []HomeAssistantTarget{{Type: "entity", ID: "light.kitchen"}},
	}}
	bridge := newTestBridge(t, ha, ledger)
	recorder := &recordingTTS{}
	bridge.tts = recorder
	mux := http.NewServeMux()
	bridge.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	missingID, _ := uuid.NewV7()
	missing := postBridge(t, server.URL+"/v1/device-agent/tts", wire.TTSRequest{RequestID: missingID.String(), Format: "wav"}, testPairingToken, "speaker-kitchen-001")
	assertBridgeError(t, missing, http.StatusNotFound, "assist_result_not_found", "not_applicable")
	if recorder.calls != 0 {
		t.Fatalf("missing claim reached TTS: %d", recorder.calls)
	}

	requestID, _ := uuid.NewV7()
	assist := wire.AssistRequest{
		RequestID: requestID.String(), SessionID: "session-tts", DeviceID: "speaker-kitchen-001",
		CommandID: "kitchen-light-off-en", RoomID: "kitchen", Text: "turn off the kitchen light", Locale: "en",
	}
	assistResponse := postBridge(t, server.URL+"/v1/device-agent/assist", assist, testPairingToken, assist.DeviceID)
	if assistResponse.Code != http.StatusOK {
		t.Fatalf("assist = %d %s", assistResponse.Code, assistResponse.Body.String())
	}

	valid := postBridge(t, server.URL+"/v1/device-agent/tts", wire.TTSRequest{RequestID: requestID.String(), Format: "wav"}, testPairingToken, assist.DeviceID)
	if valid.Code != http.StatusOK {
		t.Fatalf("tts = %d %s", valid.Code, valid.Body.String())
	}
	if recorder.calls != 1 || recorder.text != "The kitchen light is off." || recorder.locale != "en" {
		t.Fatalf("TTS calls=%d text=%q locale=%q", recorder.calls, recorder.text, recorder.locale)
	}

	raw := fmt.Sprintf(`{"request_id":%q,"format":"wav","text":"speak arbitrary text"}`, requestID.String())
	unsafeRequest := httptest.NewRequest(http.MethodPost, "/v1/device-agent/tts", bytes.NewBufferString(raw))
	unsafeRequest.RemoteAddr = "127.0.0.1:1234"
	unsafeRequest.Header.Set("Authorization", "Bearer "+testPairingToken)
	unsafeRequest.Header.Set("X-SpeechKit-Device-ID", assist.DeviceID)
	unsafe := httptest.NewRecorder()
	mux.ServeHTTP(unsafe, unsafeRequest)
	assertBridgeError(t, unsafe, http.StatusBadRequest, "request_body_invalid", "no")
	if recorder.calls != 1 {
		t.Fatalf("arbitrary TTS body reached provider: %d", recorder.calls)
	}
}

func TestBridgeRegistrationDoesNotClaimTTSReadyWhenProviderHealthFails(t *testing.T) {
	bridge := newTestBridge(t, &fakeHA{}, newFakeLedger())
	bridge.tts = fakeTTS{healthErr: errors.New("provider unavailable")}
	mux := http.NewServeMux()
	bridge.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	response := postBridge(t, server.URL+"/v1/device-agent/register", wire.Registration{
		Version: wire.CurrentProtocolVersion,
		Device:  wire.DeviceDescriptor{DeviceID: "speaker-kitchen-001", RoomID: "kitchen"},
	}, testPairingToken, "speaker-kitchen-001")
	if response.Code != http.StatusOK {
		t.Fatalf("register = %d %s", response.Code, response.Body.String())
	}
	var ack wire.RegistrationAck
	decodeRecorder(t, response, &ack)
	if ack.Capabilities.TTS.Status != wire.CapabilityUnavailable || ack.Capabilities.TTS.ReasonCode != "tts_probe_unavailable" {
		t.Fatalf("TTS capability = %#v", ack.Capabilities.TTS)
	}
}
