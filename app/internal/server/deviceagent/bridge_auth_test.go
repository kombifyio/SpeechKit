package deviceagent

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/deviceagent"
)

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
