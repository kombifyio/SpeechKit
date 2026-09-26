package deviceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/deviceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

const testPairingToken = "pairing-token-0123456789abcdefghi"

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
