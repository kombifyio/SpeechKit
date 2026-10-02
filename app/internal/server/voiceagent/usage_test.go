//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestHTTPUsageReporterSendsIdempotentConnectedMinutes(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer session-token" {
			t.Fatalf("authorization = %q", auth)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reporter := NewHTTPUsageReporter(server.URL)
	err := reporter.Report(context.Background(), "session-token", VoiceUsage{
		SessionID:   "session-123",
		AISessionID: "ai-session-456",
		Provider:    "kombify-agent",
		Duration:    90 * time.Second,
	})
	if err != nil {
		t.Fatalf("report usage: %v", err)
	}
	events, ok := got["events"].([]any)
	if !ok || len(events) != 1 {
		t.Fatalf("events = %#v", got["events"])
	}
	event := events[0].(map[string]any)
	if event["event_id"] != "voice-session:session-123:connected" {
		t.Fatalf("event_id = %#v", event["event_id"])
	}
	if event["metric"] != "audio_minutes" || event["value"] != 1.5 {
		t.Fatalf("metered event = %#v", event)
	}
}

func TestAdapterMetersOnlyProviderConnectedSessions(t *testing.T) {
	provider := newFakeProvider()
	env := startAdapterEnv(t, 0, provider, &fakeResolver{})
	start, _ := json.Marshal(StartFrame{Type: MsgStart})
	if err := env.conn.Write(context.Background(), websocket.MessageText, start); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if _, _, err := env.conn.Read(context.Background()); err != nil {
		t.Fatalf("read ready frame: %v", err)
	}
	stop, _ := json.Marshal(map[string]string{"type": MsgStop})
	if err := env.conn.Write(context.Background(), websocket.MessageText, stop); err != nil {
		t.Fatalf("stop session: %v", err)
	}
	select {
	case usage := <-env.usage:
		if usage.SessionID != "test-session" || usage.Duration <= 0 {
			t.Fatalf("usage = %#v", usage)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("connected session did not emit usage")
	}

	failed := newFakeProvider()
	failed.connectErr = errors.New("provider unavailable")
	failedEnv := startAdapterEnv(t, 0, failed, &fakeResolver{})
	if err := failedEnv.conn.Write(context.Background(), websocket.MessageText, start); err != nil {
		t.Fatalf("start failed-provider session: %v", err)
	}
	select {
	case usage := <-failedEnv.usage:
		t.Fatalf("failed provider emitted usage: %#v", usage)
	case <-failedEnv.done:
	}
}

func TestHTTPUsageReporterRetriesTransientFailureWithSameEvent(t *testing.T) {
	requests := 0
	eventIDs := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body struct {
			Events []struct {
				EventID string `json:"event_id"`
			} `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		eventIDs = append(eventIDs, body.Events[0].EventID)
		if requests == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := NewHTTPUsageReporter(server.URL).Report(context.Background(), "token", VoiceUsage{
		SessionID: "session-retry",
		Duration:  time.Minute,
	})
	if err != nil {
		t.Fatalf("report usage: %v", err)
	}
	if requests != 2 || eventIDs[0] != eventIDs[1] {
		t.Fatalf("retry requests=%d event_ids=%v", requests, eventIDs)
	}
}
