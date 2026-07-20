//go:build linux

package dictation

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

// fakeRouterWithProviders extends fakeRouter with the optional
// AvailableProviders surface the production STT router exposes, so tests can
// exercise availability-aware preference resolution.
type fakeRouterWithProviders struct {
	fakeRouter
	providers []string
}

func (f *fakeRouterWithProviders) AvailableProviders() []string {
	return append([]string(nil), f.providers...)
}

func postPrefsJSON(t *testing.T, h *Handler, prefs *middleware.VoicePrefs, extra map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	pcm := synthSine(16000, 1, 440.0, 100)
	wav := wrapWAV(pcm, 16000, 1)
	payload := map[string]any{
		"audio_base64": base64.StdEncoding.EncodeToString(wav),
		"format":       "wav",
	}
	for k, v := range extra {
		payload[k] = v
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if prefs != nil {
		req = req.WithContext(middleware.InjectVoicePrefsForTest(req.Context(), *prefs))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandler_ExplicitProviderProfileOverrideBeatsPref(t *testing.T) {
	fake := &fakeRouterWithProviders{
		fakeRouter: fakeRouter{result: okResult()},
		providers:  []string{"deepgram", "assemblyai"},
	}
	h := mustHandler(t, fake)

	prefs := &middleware.VoicePrefs{STTPrimary: "deepgram"}
	rec := postPrefsJSON(t, h, prefs, map[string]any{
		"provider_profile_id": "stt.assemblyai.universal",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastOpts.ProviderProfileID != "stt.assemblyai.universal" {
		t.Fatalf("ProviderProfileID = %q, want explicit override stt.assemblyai.universal",
			fake.lastOpts.ProviderProfileID)
	}
}

func TestHandler_UnknownExplicitProviderProfileRejected(t *testing.T) {
	fake := &fakeRouter{result: okResult()}
	h := mustHandler(t, fake)

	rec := postPrefsJSON(t, h, nil, map[string]any{
		"provider_profile_id": "stt.nonexistent.model",
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_provider_profile") {
		t.Fatalf("body should contain invalid_provider_profile, got %s", rec.Body.String())
	}
	if fake.called {
		t.Fatal("router must not be called for an invalid explicit override")
	}
}

func TestHandler_PrefPrimaryUsedWhenAvailable(t *testing.T) {
	fake := &fakeRouterWithProviders{
		fakeRouter: fakeRouter{result: okResult()},
		providers:  []string{"deepgram", "assemblyai"},
	}
	h := mustHandler(t, fake)

	prefs := &middleware.VoicePrefs{STTPrimary: "deepgram", STTSecondary: "assemblyai"}
	rec := postPrefsJSON(t, h, prefs, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastOpts.ProviderProfileID != "deepgram" {
		t.Fatalf("ProviderProfileID = %q, want deepgram", fake.lastOpts.ProviderProfileID)
	}
}

func TestHandler_PrefFallsBackToSecondaryWhenPrimaryUnavailable(t *testing.T) {
	fake := &fakeRouterWithProviders{
		fakeRouter: fakeRouter{result: okResult()},
		providers:  []string{"assemblyai", "google"},
	}
	h := mustHandler(t, fake)

	prefs := &middleware.VoicePrefs{STTPrimary: "deepgram", STTSecondary: "assemblyai"}
	rec := postPrefsJSON(t, h, prefs, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastOpts.ProviderProfileID != "assemblyai" {
		t.Fatalf("ProviderProfileID = %q, want secondary assemblyai", fake.lastOpts.ProviderProfileID)
	}
}

func TestHandler_UnsatisfiablePrefFallsBackToModelSelectionDefault(t *testing.T) {
	fake := &fakeRouterWithProviders{
		fakeRouter: fakeRouter{result: okResult()},
		providers:  []string{"google"},
	}
	h, err := New(Options{
		Router:                   fake,
		DefaultProviderProfileID: "stt.google.latest-long",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	prefs := &middleware.VoicePrefs{STTPrimary: "deepgram", STTSecondary: "assemblyai"}
	rec := postPrefsJSON(t, h, prefs, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastOpts.ProviderProfileID != "stt.google.latest-long" {
		t.Fatalf("ProviderProfileID = %q, want ModelSelection default stt.google.latest-long",
			fake.lastOpts.ProviderProfileID)
	}
}

func TestHandler_NoPrefNoDefaultLeavesRouterOrderUntouched(t *testing.T) {
	fake := &fakeRouter{result: okResult()}
	h := mustHandler(t, fake)

	rec := postPrefsJSON(t, h, nil, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastOpts.ProviderProfileID != "" {
		t.Fatalf("ProviderProfileID = %q, want empty (router default order)", fake.lastOpts.ProviderProfileID)
	}
}

func TestHandler_MultipartExplicitProviderProfile(t *testing.T) {
	fake := &fakeRouter{result: okResult()}
	h := mustHandler(t, fake)

	pcm := synthSine(16000, 1, 440.0, 100)
	wav := wrapWAV(pcm, 16000, 1)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	partHeader := map[string][]string{
		"Content-Type":        {"audio/wav"},
		"Content-Disposition": {`form-data; name="audio"; filename="hello.wav"`},
	}
	part, err := mw.CreatePart(partHeader)
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	if _, err := part.Write(wav); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.WriteField("provider_profile_id", "stt.deepgram.nova-3"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastOpts.ProviderProfileID != "stt.deepgram.nova-3" {
		t.Fatalf("ProviderProfileID = %q, want stt.deepgram.nova-3", fake.lastOpts.ProviderProfileID)
	}
}
