package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestDictationFunctionalRequiresSpokenAudioFixture(t *testing.T) {
	c := &client{base: "http://127.0.0.1:1", timeout: time.Second}
	err := scenarioDictation(c, &scenarioOpts{requireFunctional: true})
	if err == nil || !strings.Contains(err.Error(), "requires --audio-fixture") {
		t.Fatalf("scenarioDictation error = %v, want missing fixture error", err)
	}
}

func TestAssistFunctionalRejectsUnavailableProvider(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/api/v1/assist/self-test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "provider_unavailable",
				"message": "no LLM provider",
				"details": map[string]any{"provider": "missing"},
			},
		})
	})

	c := &client{base: server.URL, token: "smoke-token", timeout: 5 * time.Second}
	err := scenarioAssist(c, &scenarioOpts{requireFunctional: true})
	if err == nil || !strings.Contains(err.Error(), "unavailable during functional check") {
		t.Fatalf("scenarioAssist error = %v, want functional provider failure", err)
	}
}

func TestAssistScenarioExercisesAllDeploySmokeTools(t *testing.T) {
	var requests []map[string]any
	var selfTestCalled atomic.Bool

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/api/v1/assist/self-test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("assist self-test method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer smoke-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		selfTestCalled.Store(true)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"text":       "pong",
			"action":     "respond",
			"latency_ms": 1,
		})
	})

	mux.HandleFunc("/api/v1/assist/process", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("assist method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer smoke-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode request body %s: %v", raw, err)
		}
		requests = append(requests, body)

		action := "respond"
		if text, _ := body["text"].(string); strings.Contains(text, "copy last") || strings.Contains(text, "insert last") || strings.Contains(text, "summarize") {
			action = "execute"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"text":       "ok",
			"action":     action,
			"latency_ms": 1,
		})
	})

	c := &client{
		base:    server.URL,
		token:   "smoke-token",
		timeout: 5 * time.Second,
	}
	if err := scenarioAssist(c, &scenarioOpts{}); err != nil {
		t.Fatalf("scenarioAssist: %v", err)
	}
	if !selfTestCalled.Load() {
		t.Fatal("scenarioAssist did not call assist self-test")
	}

	wantTexts := []string{
		"what time is it",
		"copy last",
		"insert last",
		"summarize this",
	}
	if len(requests) != len(wantTexts) {
		t.Fatalf("assist requests = %d, want %d: %#v", len(requests), len(wantTexts), requests)
	}
	for i, want := range wantTexts {
		if got, _ := requests[i]["text"].(string); got != want {
			t.Fatalf("request[%d].text = %q, want %q", i, got, want)
		}
		if got := requests[i]["tts"]; got != false {
			t.Fatalf("request[%d].tts = %#v, want false", i, got)
		}
	}
	if got, _ := requests[3]["selection"].(string); !strings.Contains(got, "Deploy smoke source text") {
		t.Fatalf("summarize selection = %q, want deploy smoke source text", got)
	}
}

func TestVoiceAgentScenarioConnectsReturnedWebSocketURL(t *testing.T) {
	var wsConnected atomic.Bool
	var deleted atomic.Bool

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/voiceagent/sessions/session-1/ws"
	mux.HandleFunc("/api/v1/voiceagent/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("session create method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer smoke-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"session_id":     "session-1",
			"ws_url":         wsURL,
			"ws_subprotocol": "ticket.ticket-1",
			"legacy_ws_url":  wsURL + "?ticket=ticket-1",
			"ticket":         "ticket-1",
			"expires_at":     time.Now().Add(time.Minute).Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/api/v1/voiceagent/sessions/session-1/ws", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer smoke-token" {
			t.Fatalf("websocket Authorization = %q, want bearer token", got)
		}
		if got := r.Header.Get("Sec-WebSocket-Protocol"); !strings.Contains(got, "ticket.ticket-1") {
			t.Fatalf("websocket subprotocol = %q, want ticket.ticket-1", got)
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"ticket.ticket-1"}})
		if err != nil {
			t.Fatalf("accept websocket: %v", err)
		}
		wsConnected.Store(true)
		_ = conn.Close(websocket.StatusNormalClosure, "test complete")
	})
	mux.HandleFunc("/api/v1/voiceagent/sessions/session-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("session delete method = %s, want DELETE", r.Method)
		}
		deleted.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})

	c := &client{
		base:    server.URL,
		token:   "smoke-token",
		timeout: 5 * time.Second,
	}
	if err := scenarioVoiceAgentCreate(c, &scenarioOpts{}); err != nil {
		t.Fatalf("scenarioVoiceAgentCreate: %v", err)
	}
	if !wsConnected.Load() {
		t.Fatal("voiceagent scenario did not connect to returned ws_url")
	}
	if !deleted.Load() {
		t.Fatal("voiceagent scenario did not clean up the session")
	}
}

func TestVoiceAgentFunctionalScenarioRequiresOutputTranscript(t *testing.T) {
	var textTurnSeen atomic.Bool
	var deleted atomic.Bool

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/voiceagent/sessions/session-1/ws"
	mux.HandleFunc("/api/v1/voiceagent/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"session_id":     "session-1",
			"ws_url":         wsURL,
			"ws_subprotocol": "ticket.ticket-1",
			"legacy_ws_url":  wsURL + "?ticket=ticket-1",
			"ticket":         "ticket-1",
			"expires_at":     time.Now().Add(time.Minute).Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/api/v1/voiceagent/sessions/session-1/ws", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Sec-WebSocket-Protocol"); !strings.Contains(got, "ticket.ticket-1") {
			t.Fatalf("websocket subprotocol = %q, want ticket.ticket-1", got)
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"ticket.ticket-1"}})
		if err != nil {
			t.Fatalf("accept websocket: %v", err)
		}
		defer conn.CloseNow()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, start, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read start: %v", err)
		}
		if !strings.Contains(string(start), `"type":"start"`) {
			t.Fatalf("start frame = %s", start)
		}
		if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"state","state":"listening"}`)); err != nil {
			t.Fatalf("write state: %v", err)
		}
		_, turn, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read text turn: %v", err)
		}
		if !strings.Contains(string(turn), `"type":"text"`) {
			t.Fatalf("text frame = %s", turn)
		}
		textTurnSeen.Store(true)
		if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"output_transcript","text":"SpeechKit live check complete.","done":true}`)); err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("write transcript: %v", err)
		}
	})
	mux.HandleFunc("/api/v1/voiceagent/sessions/session-1", func(w http.ResponseWriter, r *http.Request) {
		deleted.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})

	c := &client{base: server.URL, token: "smoke-token", timeout: 5 * time.Second}
	if err := scenarioVoiceAgentCreate(c, &scenarioOpts{requireFunctional: true, voiceAgentText: "run the live check"}); err != nil {
		t.Fatalf("scenarioVoiceAgentCreate functional: %v", err)
	}
	if !textTurnSeen.Load() {
		t.Fatal("functional scenario did not send text turn")
	}
	if !deleted.Load() {
		t.Fatal("functional scenario did not clean up the session")
	}
}

func TestVoiceAgentFunctionalScenarioRequiresTextTurnBeforeOutput(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/voiceagent/sessions/session-1/ws"
	mux.HandleFunc("/api/v1/voiceagent/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"session_id":     "session-1",
			"ws_url":         wsURL,
			"ws_subprotocol": "ticket.ticket-1",
			"legacy_ws_url":  wsURL + "?ticket=ticket-1",
			"ticket":         "ticket-1",
			"expires_at":     time.Now().Add(time.Minute).Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/api/v1/voiceagent/sessions/session-1/ws", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Sec-WebSocket-Protocol"); !strings.Contains(got, "ticket.ticket-1") {
			t.Fatalf("websocket subprotocol = %q, want ticket.ticket-1", got)
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"ticket.ticket-1"}})
		if err != nil {
			t.Fatalf("accept websocket: %v", err)
		}
		defer conn.CloseNow()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, _, err := conn.Read(ctx); err != nil {
			t.Fatalf("read start: %v", err)
		}
		if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"output_transcript","text":"SpeechKit live check complete.","done":true}`)); err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("write transcript: %v", err)
		}
	})
	mux.HandleFunc("/api/v1/voiceagent/sessions/session-1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	c := &client{base: server.URL, token: "smoke-token", timeout: 5 * time.Second}
	err := scenarioVoiceAgentCreate(c, &scenarioOpts{requireFunctional: true, voiceAgentText: "run the live check"})
	if err == nil || !strings.Contains(err.Error(), "before sending text turn") {
		t.Fatalf("scenarioVoiceAgentCreate error = %v, want text-turn failure", err)
	}
}

func TestVerifyVoiceAgentWebSocketRejectsMissingURL(t *testing.T) {
	c := &client{timeout: time.Second}
	err := c.verifyVoiceAgentWebSocket(context.Background(), "", "")
	if err == nil {
		t.Fatal("verifyVoiceAgentWebSocket returned nil for empty ws_url")
	}
}
