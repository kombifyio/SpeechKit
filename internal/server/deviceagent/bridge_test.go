package deviceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/deviceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

const testPairingToken = "pairing-token-0123456789abcdefghi"

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

func TestBridgeAuthBindingCIDRAndStrictJSONFailClosed(t *testing.T) {
	bridge := newTestBridge(t, &fakeHA{}, newFakeLedger())
	mux := http.NewServeMux()
	bridge.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	body := wire.Registration{Version: wire.CurrentProtocolVersion, Device: wire.DeviceDescriptor{DeviceID: "speaker-kitchen-001", RoomID: "kitchen"}}
	wrongToken := postBridge(t, server.URL+"/v1/device-agent/register", body, "general-server-token", "speaker-kitchen-001")
	assertBridgeError(t, wrongToken, http.StatusUnauthorized, "pairing_credential_invalid", "no")

	body.Device.RoomID = "office"
	wrongRoom := postBridge(t, server.URL+"/v1/device-agent/register", body, testPairingToken, "speaker-kitchen-001")
	assertBridgeError(t, wrongRoom, http.StatusForbidden, "room_id_mismatch", "no")

	request := httptest.NewRequest(http.MethodPost, "/v1/device-agent/register", bytes.NewBufferString(`{"version":"speechkit.device_agent.v1","device":{"device_id":"speaker-kitchen-001","room_id":"kitchen"}}`))
	request.RemoteAddr = "203.0.113.10:4567"
	request.Header.Set("Authorization", "Bearer "+testPairingToken)
	request.Header.Set("X-SpeechKit-Device-ID", "speaker-kitchen-001")
	request.Header.Set("X-Forwarded-For", "127.0.0.1")
	blocked := httptest.NewRecorder()
	mux.ServeHTTP(blocked, request)
	assertBridgeError(t, blocked, http.StatusForbidden, "source_cidr_not_allowed", "no")

	unknownField := httptest.NewRequest(http.MethodPost, "/v1/device-agent/assist", bytes.NewBufferString(`{"request_id":"x","session_id":"s","device_id":"speaker-kitchen-001","room_id":"kitchen","text":"x","locale":"en","home_assistant_url":"http://evil"}`))
	unknownField.RemoteAddr = "127.0.0.1:4567"
	unknownField.Header.Set("Authorization", "Bearer "+testPairingToken)
	unknownField.Header.Set("X-SpeechKit-Device-ID", "speaker-kitchen-001")
	strict := httptest.NewRecorder()
	mux.ServeHTTP(strict, unknownField)
	assertBridgeError(t, strict, http.StatusBadRequest, "request_body_invalid", "no")

	for _, response := range []*httptest.ResponseRecorder{wrongToken, wrongRoom, blocked, strict} {
		if got := response.Header().Get(wire.ServerInstanceHeader); got != "homelab-1" {
			t.Fatalf("server identity header = %q", got)
		}
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

func TestBridgeAmbiguousDispatchNeverRedispatches(t *testing.T) {
	ledger := newFakeLedger()
	ha := &fakeHA{err: &HomeAssistantDispatchError{ReasonCode: "ha_dispatch_indeterminate", ActionExecuted: "unknown"}}
	bridge := newTestBridge(t, ha, ledger)
	mux := http.NewServeMux()
	bridge.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	id, _ := uuid.NewV7()
	request := wire.AssistRequest{
		RequestID: id.String(), SessionID: "session-1", DeviceID: "speaker-kitchen-001",
		CommandID: "kitchen-light-off-en", RoomID: "kitchen", Text: "turn off the kitchen light", Locale: "en",
	}
	first := postBridge(t, server.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
	assertBridgeError(t, first, http.StatusBadGateway, "ha_dispatch_indeterminate", "unknown")
	replay := postBridge(t, server.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
	assertBridgeError(t, replay, http.StatusConflict, "prior_dispatch_outcome_unknown", "unknown")
	if ha.converseCalls != 1 {
		t.Fatalf("HA calls = %d, want exactly 1", ha.converseCalls)
	}
}

func TestBridgeHomeAssistantErrorIsTerminalSpokenResult(t *testing.T) {
	ledger := newFakeLedger()
	ha := &fakeHA{result: &HomeAssistantResult{
		ResponseType: "error", Speech: "I could not find that device.", ErrorCode: "home_assistant_rejected",
		ReasonCode: "ha_no_intent_match", ActionExecuted: "no",
	}}
	bridge := newTestBridge(t, ha, ledger)
	mux := http.NewServeMux()
	bridge.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	id, _ := uuid.NewV7()
	request := wire.AssistRequest{
		RequestID: id.String(), SessionID: "session-1", DeviceID: "speaker-kitchen-001",
		CommandID: "unknown-light-off-en", RoomID: "kitchen", Text: "turn off the unknown light", Locale: "en",
	}
	response := postBridge(t, server.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
	if response.Code != http.StatusOK {
		t.Fatalf("assist = %d %s", response.Code, response.Body.String())
	}
	var result wire.AssistResponse
	decodeRecorder(t, response, &result)
	if result.Status != "denied" || result.Speech == "" || result.ReasonCode != "ha_no_intent_match" || result.Retryable {
		t.Fatalf("result = %#v", result)
	}
	if ha.converseCalls != 1 {
		t.Fatalf("HA calls = %d", ha.converseCalls)
	}
	if ha.verifyCalls != 0 {
		t.Fatalf("HA state verification calls = %d, want 0", ha.verifyCalls)
	}
}

func TestBridgeTargetMismatchIsIndeterminateAndNeverVerifiesOrRedispatches(t *testing.T) {
	ledger := newFakeLedger()
	ha := &fakeHA{result: &HomeAssistantResult{
		ResponseType: "action_done", Speech: "Done.", ActionExecuted: "unknown",
		SuccessTargets: []HomeAssistantTarget{{Type: "entity", ID: "light.office"}},
	}}
	bridge := newTestBridge(t, ha, ledger)
	mux := http.NewServeMux()
	bridge.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	id, _ := uuid.NewV7()
	request := wire.AssistRequest{
		RequestID: id.String(), SessionID: "session-1", DeviceID: "speaker-kitchen-001",
		CommandID: "kitchen-light-off-en", RoomID: "kitchen", Text: "turn off the kitchen light", Locale: "en",
	}
	first := postBridge(t, server.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
	assertBridgeError(t, first, http.StatusBadGateway, "ha_authorized_target_unverified", "unknown")
	replay := postBridge(t, server.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
	assertBridgeError(t, replay, http.StatusConflict, "prior_dispatch_outcome_unknown", "unknown")
	if ha.converseCalls != 1 || ha.verifyCalls != 0 {
		t.Fatalf("HA converse=%d verify=%d", ha.converseCalls, ha.verifyCalls)
	}
}

func TestBridgeStateMismatchIsIndeterminateAndNeverRedispatches(t *testing.T) {
	ledger := newFakeLedger()
	ha := &fakeHA{
		result: &HomeAssistantResult{
			ResponseType: "action_done", Speech: "Done.", ActionExecuted: "unknown",
			SuccessTargets: []HomeAssistantTarget{{Type: "entity", ID: "light.kitchen"}},
		},
		verifyErr: &HomeAssistantDispatchError{ReasonCode: "ha_state_mismatch", ActionExecuted: "unknown"},
	}
	bridge := newTestBridge(t, ha, ledger)
	mux := http.NewServeMux()
	bridge.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	id, _ := uuid.NewV7()
	request := wire.AssistRequest{
		RequestID: id.String(), SessionID: "session-1", DeviceID: "speaker-kitchen-001",
		CommandID: "kitchen-light-off-en", RoomID: "kitchen", Text: "turn off the kitchen light", Locale: "en",
	}
	first := postBridge(t, server.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
	assertBridgeError(t, first, http.StatusBadGateway, "ha_state_mismatch", "unknown")
	replay := postBridge(t, server.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
	assertBridgeError(t, replay, http.StatusConflict, "prior_dispatch_outcome_unknown", "unknown")
	if ha.converseCalls != 1 || ha.verifyCalls != 1 || ha.verifyEntity != "light.kitchen" || ha.verifyState != "off" {
		t.Fatalf("HA converse=%d verify=%d entity=%q state=%q", ha.converseCalls, ha.verifyCalls, ha.verifyEntity, ha.verifyState)
	}
}

func TestBridgeRejectsInvalidUUIDv7WindowBeforeClaimOrHA(t *testing.T) {
	ledger := newFakeLedger()
	ha := &fakeHA{}
	now := time.Now().UTC()
	bridge := newTestBridgeWithNow(t, ha, ledger, func() time.Time { return now })
	mux := http.NewServeMux()
	bridge.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	for _, requestID := range []string{"not-a-uuid", uuid.New().String()} {
		request := wire.AssistRequest{
			RequestID: requestID, SessionID: "session-1", DeviceID: "speaker-kitchen-001",
			CommandID: "kitchen-light-off-en", RoomID: "kitchen", Text: "turn off the kitchen light", Locale: "en",
		}
		response := postBridge(t, server.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
		assertBridgeError(t, response, http.StatusUnprocessableEntity, "request_id_not_uuidv7", "no")
	}
	if ledger.claimCalls != 0 || ha.converseCalls != 0 {
		t.Fatalf("claim calls=%d HA calls=%d", ledger.claimCalls, ha.converseCalls)
	}
}

func TestBridgeLocalPolicyDeniesBeforeClaimOrHomeAssistant(t *testing.T) {
	ledger := newFakeLedger()
	ha := &fakeHA{}
	bridge := newTestBridge(t, ha, ledger)
	mux := http.NewServeMux()
	bridge.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	id, _ := uuid.NewV7()
	request := wire.AssistRequest{
		RequestID: id.String(), SessionID: "session-1", CommandID: "not-authorized",
		DeviceID: "speaker-kitchen-001", RoomID: "kitchen", Text: "unlock the front door", Locale: "en",
	}
	response := postBridge(t, server.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
	assertBridgeError(t, response, http.StatusForbidden, PolicyReasonRuleNotFound, "no")
	if ledger.claimCalls != 0 || ha.converseCalls != 0 {
		t.Fatalf("denied policy request reached claim/HA: claim=%d HA=%d", ledger.claimCalls, ha.converseCalls)
	}
}

func TestNewBridgeRejectsUnsafeOrAliasedBindings(t *testing.T) {
	now := time.Now().UTC()
	policy, err := NewPolicy(RuleOptions{
		RuleID: "light-off", DeviceID: "device-one", RoomID: "room-one", TriggerText: "light off",
		Locale: "en", Action: ActionTurnOff, EntityID: "light.one", NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	base := BridgeOptions{
		ServerInstanceID: "server-one", HomeAssistant: &fakeHA{}, TTS: fakeTTS{}, TTSReady: true,
		Claims: newFakeLedger(), Policy: policy, MaxRequestAge: time.Minute,
		Bindings: []DeviceBindingOptions{{
			PairingID: "pair-one", DeviceID: "device-one", RoomID: "room-one",
			Token: testPairingToken, AllowedClientCIDRs: []string{"127.0.0.1/32"},
		}},
	}
	tests := []struct {
		name   string
		mutate func(*BridgeOptions)
	}{
		{name: "public CIDR", mutate: func(opts *BridgeOptions) { opts.Bindings[0].AllowedClientCIDRs = []string{"0.0.0.0/0"} }},
		{name: "header unsafe server id", mutate: func(opts *BridgeOptions) { opts.ServerInstanceID = "server\r\nevil" }},
		{name: "duplicate pairing", mutate: func(opts *BridgeOptions) {
			opts.Bindings = append(opts.Bindings, DeviceBindingOptions{PairingID: "pair-one", DeviceID: "device-two", RoomID: "room-two", Token: "second-pairing-token-0123456789abc", AllowedClientCIDRs: []string{"127.0.0.1/32"}})
		}},
		{name: "duplicate token", mutate: func(opts *BridgeOptions) {
			opts.Bindings = append(opts.Bindings, DeviceBindingOptions{PairingID: "pair-two", DeviceID: "device-two", RoomID: "room-two", Token: testPairingToken, AllowedClientCIDRs: []string{"127.0.0.1/32"}})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := base
			opts.Bindings = append([]DeviceBindingOptions(nil), base.Bindings...)
			tc.mutate(&opts)
			if _, err := NewBridge(opts); err == nil {
				t.Fatal("NewBridge accepted unsafe binding")
			}
		})
	}
}

func newTestBridge(t *testing.T, ha HomeAssistant, ledger ClaimLedger) *Bridge {
	t.Helper()
	return newTestBridgeWithNow(t, ha, ledger, time.Now)
}

func newTestBridgeWithNow(t *testing.T, ha HomeAssistant, ledger ClaimLedger, now func() time.Time) *Bridge {
	t.Helper()
	policyNow := now().UTC()
	policy, err := NewPolicy(
		RuleOptions{RuleID: "kitchen-light-off-de", DeviceID: "speaker-kitchen-001", RoomID: "kitchen", TriggerText: "mach das licht aus", Locale: "de-DE", Action: ActionTurnOff, EntityID: "light.kitchen", NotBefore: policyNow.Add(-time.Hour), ExpiresAt: policyNow.Add(time.Hour)},
		RuleOptions{RuleID: "kitchen-light-on-de", DeviceID: "speaker-kitchen-001", RoomID: "kitchen", TriggerText: "mach das licht an", Locale: "de-DE", Action: ActionTurnOn, EntityID: "light.kitchen", NotBefore: policyNow.Add(-time.Hour), ExpiresAt: policyNow.Add(time.Hour)},
		RuleOptions{RuleID: "kitchen-light-off-en", DeviceID: "speaker-kitchen-001", RoomID: "kitchen", TriggerText: "turn off the kitchen light", Locale: "en", Action: ActionTurnOff, EntityID: "light.kitchen", NotBefore: policyNow.Add(-time.Hour), ExpiresAt: policyNow.Add(time.Hour)},
		RuleOptions{RuleID: "unknown-light-off-en", DeviceID: "speaker-kitchen-001", RoomID: "kitchen", TriggerText: "turn off the unknown light", Locale: "en", Action: ActionTurnOff, EntityID: "light.unknown", NotBefore: policyNow.Add(-time.Hour), ExpiresAt: policyNow.Add(time.Hour)},
	)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	bridge, err := NewBridge(BridgeOptions{
		ServerInstanceID: "homelab-1",
		Bindings: []DeviceBindingOptions{{
			PairingID: "pairing-kitchen-v1", DeviceID: "speaker-kitchen-001", RoomID: "kitchen",
			Token: testPairingToken, AllowedClientCIDRs: []string{"127.0.0.0/8", "192.168.10.42/32"},
		}},
		HomeAssistant: ha,
		TTS:           fakeTTS{},
		TTSReady:      true,
		Claims:        ledger,
		Policy:        policy,
		MaxRequestAge: 10 * time.Minute,
		FutureSkew:    2 * time.Minute,
		Now:           now,
	})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	return bridge
}

func postBridge(t *testing.T, endpoint string, body any, token, deviceID string) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(encoded))
	req.RequestURI = ""
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-SpeechKit-Device-ID", deviceID)
	response := httptest.NewRecorder()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return http.DefaultTransport.RoundTrip(r)
	})}
	actual, err := client.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer actual.Body.Close() //nolint:errcheck // test body is copied below
	response.Code = actual.StatusCode
	for key, values := range actual.Header {
		for _, value := range values {
			response.Header().Add(key, value)
		}
	}
	_, _ = response.Body.ReadFrom(actual.Body)
	return response
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func decodeRecorder(t *testing.T, response *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}

func assertBridgeError(t *testing.T, response *httptest.ResponseRecorder, status int, reason, action string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d want %d body=%s", response.Code, status, response.Body.String())
	}
	var envelope wire.ErrorEnvelope
	decodeRecorder(t, response, &envelope)
	if envelope.Error.ReasonCode != reason || envelope.Error.ActionExecuted != action {
		t.Fatalf("error = %#v", envelope.Error)
	}
}

type fakeHA struct {
	probeErr      error
	result        *HomeAssistantResult
	err           error
	verifyErr     error
	probeCalls    int
	converseCalls int
	verifyCalls   int
	verifyEntity  string
	verifyState   string
}

func (h *fakeHA) Probe(context.Context) error {
	h.probeCalls++
	return h.probeErr
}

func (h *fakeHA) Converse(context.Context, string, string) (*HomeAssistantResult, error) {
	h.converseCalls++
	return h.result, h.err
}

func (h *fakeHA) VerifyState(_ context.Context, entityID, expectedState string) error {
	h.verifyCalls++
	h.verifyEntity = entityID
	h.verifyState = expectedState
	return h.verifyErr
}

type fakeTTS struct{ healthErr error }

func (fakeTTS) Synthesize(context.Context, string, tts.SynthesizeOpts) (*tts.Result, error) {
	return &tts.Result{Audio: make([]byte, 44), Format: "wav", SampleRate: 16000, Provider: "fake-piper"}, nil
}

func (f fakeTTS) ReadyHealthCheck(context.Context) map[string]error {
	return map[string]error{"fake-piper": f.healthErr}
}

type recordingTTS struct {
	calls  int
	text   string
	locale string
}

func (r *recordingTTS) Synthesize(_ context.Context, text string, opts tts.SynthesizeOpts) (*tts.Result, error) {
	r.calls++
	r.text = text
	r.locale = opts.Locale
	return &tts.Result{Audio: make([]byte, 44), Format: "wav", SampleRate: 16000, Provider: "recording-tts"}, nil
}

func (*recordingTTS) ReadyHealthCheck(context.Context) map[string]error {
	return map[string]error{"recording-tts": nil}
}

type fakeLedgerRecord struct {
	digest        [32]byte
	state         string
	result        StoredResult
	indeterminate string
}

type fakeLedger struct {
	mu         sync.Mutex
	records    map[ClaimKey]fakeLedgerRecord
	claimCalls int
}

func newFakeLedger() *fakeLedger { return &fakeLedger{records: map[ClaimKey]fakeLedgerRecord{}} }

func (l *fakeLedger) Claim(_ context.Context, key ClaimKey, digest [32]byte, _ time.Time) (ClaimDecision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.claimCalls++
	record, ok := l.records[key]
	if !ok {
		l.records[key] = fakeLedgerRecord{digest: digest, state: "claimed"}
		return ClaimDecision{Disposition: ClaimDispatchNew}, nil
	}
	if record.digest != digest {
		return ClaimDecision{Disposition: ClaimDigestConflict}, nil
	}
	switch record.state {
	case "completed":
		result := record.result
		return ClaimDecision{Disposition: ClaimReplayCompleted, Result: &result}, nil
	default:
		return ClaimDecision{Disposition: ClaimIndeterminate}, nil
	}
}

func (l *fakeLedger) Lookup(_ context.Context, key ClaimKey, _ time.Time) (ClaimDecision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	record, ok := l.records[key]
	if !ok {
		return ClaimDecision{Disposition: ClaimNotFound}, nil
	}
	if record.state == "completed" {
		result := record.result
		return ClaimDecision{Disposition: ClaimReplayCompleted, Result: &result}, nil
	}
	return ClaimDecision{Disposition: ClaimIndeterminate}, nil
}

func (l *fakeLedger) Complete(_ context.Context, key ClaimKey, digest [32]byte, result StoredResult, _ time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	record, ok := l.records[key]
	if !ok || record.digest != digest || record.state != "claimed" {
		return errors.New("claim not dispatchable")
	}
	record.state = "completed"
	record.result = result
	l.records[key] = record
	return nil
}

func (l *fakeLedger) MarkIndeterminate(_ context.Context, key ClaimKey, digest [32]byte, reason string, _ time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	record, ok := l.records[key]
	if !ok || record.digest != digest {
		return errors.New("claim not found")
	}
	record.state = "indeterminate"
	record.indeterminate = reason
	l.records[key] = record
	return nil
}
