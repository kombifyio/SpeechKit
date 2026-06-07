//go:build linux

package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthRegistry_InitialOverallIsStarting(t *testing.T) {
	r := NewHealthRegistry()
	overall, components, _ := r.Snapshot()
	if overall != StatusStarting {
		t.Fatalf("empty registry should report StatusStarting, got %q", overall)
	}
	if len(components) != 0 {
		t.Fatalf("empty registry should have zero components, got %d", len(components))
	}
}

func TestHealthRegistry_SetReady(t *testing.T) {
	r := NewHealthRegistry()
	r.SetReady("server", StatusOK, "listening")
	overall, components, _ := r.Snapshot()
	if overall != StatusOK {
		t.Fatalf("single OK component should make overall OK, got %q", overall)
	}
	if got := components["server"].Status; got != StatusOK {
		t.Fatalf("server component should be OK, got %q", got)
	}
	if got := components["server"].Detail; got != "listening" {
		t.Fatalf("detail should be propagated, got %q", got)
	}
}

func TestHealthRegistry_WorstStatusWins(t *testing.T) {
	r := NewHealthRegistry()
	r.SetReady("a", StatusOK, "")
	r.SetReady("b", StatusDegraded, "slow")
	r.SetReady("c", StatusUnavailable, "down")
	overall, _, _ := r.Snapshot()
	if overall != StatusUnavailable {
		t.Fatalf("expected worst status (unavailable) to win, got %q", overall)
	}
}

func TestHealthRegistry_ReadinessIgnoresNonBlockingComponents(t *testing.T) {
	r := NewHealthRegistry()
	r.SetReady("server", StatusOK, "listening")
	r.SetReadyWithOptions("stt.google", StatusDegraded, "403", ComponentOptions{
		Blocking: false,
		Kind:     "provider",
		Provider: "google",
	})

	strict, components, _ := r.Snapshot()
	if strict != StatusDegraded {
		t.Fatalf("strict status = %q, want degraded", strict)
	}
	if components["stt.google"].Blocking {
		t.Fatalf("stt.google should be non-blocking")
	}

	ready, _, _ := r.ReadinessSnapshot()
	if ready != StatusOK {
		t.Fatalf("readiness status = %q, want ok", ready)
	}
}

func TestRegisterHealth_StrictReadinessEndpointsIncludeOptionalFailures(t *testing.T) {
	app := &App{
		Mux:     http.NewServeMux(),
		Health:  NewHealthRegistry(),
		Version: "test-version",
	}
	app.Health.SetReady("server", StatusOK, "listening")
	app.Health.SetReadyWithOptions("stt.google", StatusDegraded, "403", ComponentOptions{
		Blocking: false,
		Kind:     "provider",
		Modes:    []string{string(ModeDictation)},
		Provider: "google",
	})
	registerHealth(app)

	readyRec := httptest.NewRecorder()
	app.Mux.ServeHTTP(readyRec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if readyRec.Code != http.StatusOK {
		t.Fatalf("/readyz status = %d body=%s", readyRec.Code, readyRec.Body.String())
	}
	ready := decodeReadinessBody(t, readyRec.Body.Bytes())
	if ready.Status != string(StatusOK) || ready.StrictStatus != string(StatusDegraded) {
		t.Fatalf("/readyz body = %+v, want status ok and strict_status degraded", ready)
	}
	if ready.Components["stt.google"].Blocking {
		t.Fatalf("stt.google should stay visible as non-blocking in /readyz")
	}

	for _, endpoint := range []string{"/readyz?strict=true", "/readyz/strict"} {
		rec := httptest.NewRecorder()
		app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, endpoint, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d body=%s", endpoint, rec.Code, rec.Body.String())
		}
		body := decodeReadinessBody(t, rec.Body.Bytes())
		if body.Status != string(StatusDegraded) || body.StrictStatus != string(StatusDegraded) {
			t.Fatalf("%s body = %+v, want degraded/degraded", endpoint, body)
		}
	}
}

type readinessBodyForTest struct {
	Status       string                    `json:"status"`
	StrictStatus string                    `json:"strict_status"`
	Components   map[string]ComponentEntry `json:"components"`
}

func decodeReadinessBody(t *testing.T, data []byte) readinessBodyForTest {
	t.Helper()
	var body readinessBodyForTest
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode readiness body: %v body=%s", err, data)
	}
	return body
}

func TestResolveModes_EmptyMeansAll(t *testing.T) {
	modes := resolveModes(nil)
	for _, want := range []Mode{ModeDictation, ModeAssist, ModeVoiceAgent} {
		if !modes[want] {
			t.Fatalf("mode %q should default to enabled", want)
		}
	}
}

func TestResolveModes_Subset(t *testing.T) {
	modes := resolveModes([]string{"dictation", "voiceagent"})
	if !modes[ModeDictation] || !modes[ModeVoiceAgent] {
		t.Fatalf("configured modes should be enabled")
	}
	if modes[ModeAssist] {
		t.Fatalf("assist should stay disabled when not listed")
	}
}

func TestResolveModes_Normalizes(t *testing.T) {
	modes := resolveModes([]string{"DICTATION", "  Assist "})
	if !modes[ModeDictation] || !modes[ModeAssist] {
		t.Fatalf("case and whitespace should be tolerated")
	}
}

func TestApp_ModeEnabled(t *testing.T) {
	app := &App{Modes: map[Mode]bool{ModeAssist: true, ModeDictation: false}}
	if !app.ModeEnabled(ModeAssist) {
		t.Fatalf("expected ModeEnabled(assist) to be true")
	}
	if app.ModeEnabled(ModeDictation) {
		t.Fatalf("expected ModeEnabled(dictation) to be false")
	}
	var nilApp *App
	if nilApp.ModeEnabled(ModeAssist) {
		t.Fatalf("nil App should always report false")
	}
}
