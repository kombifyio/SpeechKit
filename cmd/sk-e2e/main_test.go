package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestAssistScenarioExercisesAllDeploySmokeTools(t *testing.T) {
	var requests []map[string]any

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

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

func TestSelectedScenariosAudioFixtureAndErrorEnvelope(t *testing.T) {
	if got := selectedScenarios(" all "); strings.Join(got, ",") != "health,dictation,assist,voiceagent" {
		t.Fatalf("selectedScenarios(all) = %#v", got)
	}
	if got := selectedScenarios(" health, dictation ,, assist "); strings.Join(got, ",") != "health,dictation,assist" {
		t.Fatalf("selectedScenarios(custom) = %#v", got)
	}

	if !hasErrorEnvelope([]byte(`{"error":{"code":"missing_provider","message":"no STT"}}`)) {
		t.Fatal("valid error envelope was not detected")
	}
	if hasErrorEnvelope([]byte(`{"error":{"message":"missing code"}}`)) {
		t.Fatal("error envelope without code should be rejected")
	}
	if hasErrorEnvelope([]byte(`not-json`)) {
		t.Fatal("invalid JSON should not be an error envelope")
	}

	pcm := synthSine(16000, 440, 250)
	if len(pcm) != 8000 {
		t.Fatalf("pcm bytes = %d, want 8000", len(pcm))
	}
	wav := wrapWAV(pcm, 16000, 1)
	if string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" || string(wav[36:40]) != "data" {
		t.Fatalf("wav header is malformed: %q %q %q", wav[:4], wav[8:12], wav[36:40])
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != 16000 {
		t.Fatalf("wav sample rate = %d, want 16000", got)
	}
	if got := binary.LittleEndian.Uint32(wav[40:44]); got != uint32(len(pcm)) {
		t.Fatalf("wav data size = %d, want %d", got, len(pcm))
	}
}

func TestHealthScenarioAllowsDegradedReadyUnlessStrict(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "degraded"})
	})

	c := &client{base: server.URL, timeout: 5 * time.Second}
	if err := scenarioHealth(c, &scenarioOpts{}); err != nil {
		t.Fatalf("scenarioHealth degraded readyz: %v", err)
	}
	if err := scenarioHealth(c, &scenarioOpts{strictReady: true}); err == nil || !strings.Contains(err.Error(), "--strict-ready") {
		t.Fatalf("scenarioHealth strict readyz error = %v, want strict-ready failure", err)
	}
}

func TestDictationScenarioAcceptsOKAndDegradedEnvelopes(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	var sawOKRequest atomic.Bool
	mux.HandleFunc("/api/v1/dictation/transcribe", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer smoke-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Fatalf("Content-Type = %q, want multipart", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		if got := r.FormValue("language"); got != "en" {
			t.Fatalf("language = %q, want en", got)
		}
		file, _, err := r.FormFile("audio")
		if err != nil {
			t.Fatalf("FormFile(audio): %v", err)
		}
		defer func() { _ = file.Close() }()
		header := make([]byte, 12)
		if _, err := io.ReadFull(file, header); err != nil {
			t.Fatalf("read wav header: %v", err)
		}
		if string(header[:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
			t.Fatalf("uploaded audio is not a WAV fixture: %q", header)
		}
		sawOKRequest.Store(true)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"text":        "fixture",
			"duration_ms": 250,
			"latency_ms":  12,
			"provider":    "test",
		})
	})

	c := &client{base: server.URL, token: "smoke-token", timeout: 5 * time.Second}
	if err := scenarioDictation(c, &scenarioOpts{}); err != nil {
		t.Fatalf("scenarioDictation ok: %v", err)
	}
	if !sawOKRequest.Load() {
		t.Fatal("dictation scenario did not send the multipart request")
	}

	degraded := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"code":    "missing_provider",
				"message": "no STT provider configured",
			},
		})
	}))
	defer degraded.Close()

	c = &client{base: degraded.URL, token: "smoke-token", timeout: 5 * time.Second}
	if err := scenarioDictation(c, &scenarioOpts{}); err != nil {
		t.Fatalf("scenarioDictation degraded envelope: %v", err)
	}
}

func TestVoiceAgentScenarioConnectsReturnedWebSocketURL(t *testing.T) {
	var wsConnected atomic.Bool
	var deleted atomic.Bool

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/voiceagent/sessions/session-1/ws?ticket=ticket-1"
	mux.HandleFunc("/api/v1/voiceagent/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("session create method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer smoke-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"session_id": "session-1",
			"ws_url":     wsURL,
			"ticket":     "ticket-1",
			"expires_at": time.Now().Add(time.Minute).Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/api/v1/voiceagent/sessions/session-1/ws", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer smoke-token" {
			t.Fatalf("websocket Authorization = %q, want bearer token", got)
		}
		conn, err := websocket.Accept(w, r, nil)
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

func TestVerifyVoiceAgentWebSocketRejectsMissingURL(t *testing.T) {
	c := &client{timeout: time.Second}
	err := c.verifyVoiceAgentWebSocket(context.Background(), "")
	if err == nil {
		t.Fatal("verifyVoiceAgentWebSocket returned nil for empty ws_url")
	}
}
