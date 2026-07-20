package deviceagent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kombifyio/SpeechKit/internal/server/deviceagent/claimstore"
	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/deviceagent"
)

func TestDurableClaimLedgerReplaysAcrossBridgeRestartWithoutRedispatch(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	request := wire.AssistRequest{
		RequestID: id.String(), SessionID: "session-restart", DeviceID: "speaker-kitchen-001",
		CommandID: "kitchen-light-off-en", RoomID: "kitchen", Text: "turn off the kitchen light", Locale: "en",
	}
	ha := &fakeHA{result: &HomeAssistantResult{
		ConversationID: "ha-conversation-restart-1", ResponseType: "action_done", Speech: "The kitchen light is off.", ActionExecuted: "unknown",
		SuccessTargets: []HomeAssistantTarget{{Type: "entity", ID: "light.kitchen"}},
	}}
	path := filepath.Join(t.TempDir(), "device-agent-claims.sqlite")

	serve := func(t *testing.T) (*httptest.Server, *claimstore.Ledger) {
		t.Helper()
		store, err := claimstore.Open(context.Background(), claimstore.Options{
			Path:          path,
			MaxEntries:    100,
			Retention:     time.Hour,
			MaxRequestAge: 10 * time.Minute,
			FutureSkew:    2 * time.Minute,
		})
		if err != nil {
			t.Fatalf("open claim store: %v", err)
		}
		ledger, err := NewDurableClaimLedger(store)
		if err != nil {
			_ = store.Close()
			t.Fatalf("new durable ledger: %v", err)
		}
		bridge := newTestBridgeWithNow(t, ha, ledger, func() time.Time { return now })
		mux := http.NewServeMux()
		bridge.Mount(mux)
		return httptest.NewServer(mux), store
	}

	firstServer, firstStore := serve(t)
	first := postBridge(t, firstServer.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
	if first.Code != http.StatusOK {
		t.Fatalf("first assist = %d %s", first.Code, first.Body.String())
	}
	var firstResult wire.AssistResponse
	decodeRecorder(t, first, &firstResult)
	if firstResult.ConversationID != ha.result.ConversationID {
		t.Fatalf("first conversation id = %q, want %q", firstResult.ConversationID, ha.result.ConversationID)
	}
	firstServer.Close()
	if err := firstStore.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	restartedServer, restartedStore := serve(t)
	defer restartedServer.Close()
	defer restartedStore.Close() //nolint:errcheck // test cleanup
	replay := postBridge(t, restartedServer.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay assist = %d %s", replay.Code, replay.Body.String())
	}
	var result wire.AssistResponse
	decodeRecorder(t, replay, &result)
	if !result.Replayed || result.Speech != firstResult.Speech || result.ConversationID != firstResult.ConversationID {
		t.Fatalf("replay = %#v", result)
	}
	if ha.converseCalls != 1 {
		t.Fatalf("HA calls after restart replay = %d, want 1", ha.converseCalls)
	}
	if ha.verifyCalls != 1 {
		t.Fatalf("HA state verification calls after restart replay = %d, want 1", ha.verifyCalls)
	}
}
