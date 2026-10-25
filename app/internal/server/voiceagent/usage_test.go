//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/app/internal/server/wssession"
)

func TestHTTPUsageReporterSendsIdempotentConnectedMinutes(t *testing.T) {
	const receipt = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
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
	usage := VoiceUsage{
		SessionID:     "session-123",
		AISessionID:   "ai-session-456",
		Provider:      "kombify-agent",
		Duration:      90 * time.Second,
		ReservationID: "reservation-123",
		ReceiptToken:  receipt,
	}
	err := reporter.Report(context.Background(), "session-token", usage)
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
	if got["reservation_id"] != "reservation-123" {
		t.Fatal("receipt did not settle its reserved quota")
	}
	assertReceipt := func() {
		t.Helper()
		if got["reservation_receipt"] != receipt {
			t.Fatal("terminal broker request lost its reservation capability")
		}
		nested, err := json.Marshal(got["events"])
		if err != nil || strings.Contains(string(nested), receipt) || strings.Contains(string(nested), "reservation_receipt") {
			t.Fatal("reservation capability escaped into event metadata")
		}
	}
	assertReceipt()
	for name, value := range map[string]any{
		"usage":           usage,
		"session budget":  wssession.VoiceBudget{ReservationID: usage.ReservationID, ReceiptToken: receipt},
		"managed session": ManagedSession{VoiceBudget: wssession.VoiceBudget{ReservationID: usage.ReservationID, ReceiptToken: receipt}},
	} {
		public, err := json.Marshal(value)
		if err != nil || strings.Contains(string(public), receipt) || strings.Contains(string(public), "ReceiptToken") || strings.Contains(string(public), "reservation_receipt") {
			t.Fatalf("reservation capability escaped into public %s JSON", name)
		}
	}
	usage.Duration = 0
	if err := reporter.Report(context.Background(), "session-token", usage); err != nil {
		t.Fatalf("trusted zero settlement failed: %v", err)
	}
	assertReceipt()
	zeroEvent := got["events"].([]any)[0].(map[string]any)
	if zeroEvent["value"] != float64(0) || zeroEvent["event_id"] != event["event_id"] || got["reservation_id"] != usage.ReservationID {
		t.Fatal("zero settlement changed the reserved accounting event")
	}
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, destination := range []string{"same_origin_route", "other_host"} {
			t.Run("redirect_"+strconv.Itoa(status)+"_"+destination, func(t *testing.T) {
				var redirected, callbacks atomic.Int32
				targetHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					redirected.Add(1)
					w.WriteHeader(http.StatusOK)
				})
				target := httptest.NewServer(targetHandler)
				defer target.Close()
				var broker *httptest.Server
				broker = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/redirected" {
						targetHandler.ServeHTTP(w, r)
						return
					}
					location := broker.URL + "/redirected"
					if destination == "other_host" {
						location = strings.Replace(target.URL, "127.0.0.1", "localhost", 1) + "/redirected"
					}
					http.Redirect(w, r, location, status)
				}))
				defer broker.Close()
				reporter := NewHTTPUsageReporter(broker.URL)
				// A custom permissive client must not override capability custody.
				// The reporter must also leave that shared client unchanged for v1.
				reporter.Client.CheckRedirect = func(*http.Request, []*http.Request) error {
					callbacks.Add(1)
					return nil
				}
				if err := reporter.Report(context.Background(), "session-token", usage); err == nil {
					t.Error("receipt-bearing report accepted a redirect")
				}
				if redirected.Load() != 0 || callbacks.Load() != 0 {
					t.Error("receipt-bearing report followed a redirect")
				}
				// The same destination is reachable and the pre-existing v1
				// redirect behavior remains available without a receipt.
				before := redirected.Load()
				legacy := usage
				legacy.ReceiptToken = ""
				if err := reporter.Report(context.Background(), "session-token", legacy); err != nil {
					t.Fatalf("legacy redirect compatibility: %v", err)
				}
				if redirected.Load() != before+1 || callbacks.Load() == 0 {
					t.Fatal("legacy control did not reach the redirect destination")
				}
			})
		}
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
		if usage.SessionID != "test-session" || usage.Duration <= 0 || usage.OwnerUserID != "u1" {
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
	reservedFailure := newFakeProvider()
	reservedFailure.connectErr = errors.New("provider unavailable")
	reservedEnv := startAdapterEnv(t, 0, reservedFailure, &fakeResolver{}, "reserved-failure")
	if err := reservedEnv.conn.Write(context.Background(), websocket.MessageText, start); err != nil {
		t.Fatal(err)
	}
	select {
	case usage := <-reservedEnv.usage:
		if usage.ReservationID != "reserved-failure" || usage.Duration != 0 || usage.OwnerUserID != "u1" {
			t.Fatalf("failed reserved connection did not settle trusted zero: %+v", usage)
		}
	case <-time.After(time.Second):
		t.Fatal("failed reserved connection retained its hold without a final receipt")
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
