package deviceagent

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/deviceagent"
)

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
