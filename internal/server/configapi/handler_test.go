//go:build linux

package configapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func newHandler(ready func() string) *Handler {
	cfg := &config.Config{}
	cfg.Server.PublicURL = "https://api.example.com/"
	cfg.Server.Modes = []string{"dictation", "assist"}
	cfg.Server.AuthMode = "bearer"
	cfg.Server.BearerRole = "service"
	cfg.Server.MaxUploadMB = 25
	cfg.Server.ReadHeaderTimeoutSec = 15
	cfg.Server.ReadTimeoutSec = 120
	cfg.Server.IdleTimeoutSec = 120
	cfg.Server.MaxHeaderBytes = 1 << 20
	cfg.Server.MaxDecodedAudioSeconds = 600
	cfg.Server.Features.Catalog = true
	cfg.Server.Features.Vocabulary = true
	return New(cfg, "v1.2.3", ready)
}

func TestConfig_GET_ReturnsPublicView(t *testing.T) {
	h := newHandler(func() string { return "ready" })

	rec := httptest.NewRecorder()
	h.config(rec, httptest.NewRequest(http.MethodGet, "/v1/config", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}

	var body struct {
		Version                string          `json:"version"`
		PublicURL              string          `json:"public_url"`
		AuthMode               string          `json:"auth_mode"`
		MaxUploadMB            int             `json:"max_upload_mb"`
		ReadHeaderTimeoutSec   int             `json:"read_header_timeout_sec"`
		ReadTimeoutSec         int             `json:"read_timeout_sec"`
		IdleTimeoutSec         int             `json:"idle_timeout_sec"`
		MaxHeaderBytes         int             `json:"max_header_bytes"`
		MaxDecodedAudioSeconds int             `json:"max_decoded_audio_seconds"`
		Features               map[string]bool `json:"features"`
		ReadyStatus            string          `json:"ready_status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Version != "v1.2.3" {
		t.Errorf("version = %q, want v1.2.3", body.Version)
	}
	// trailing slash must be trimmed by the handler.
	if body.PublicURL != "https://api.example.com" {
		t.Errorf("public_url = %q, want trimmed origin", body.PublicURL)
	}
	if body.AuthMode != "bearer" {
		t.Errorf("auth_mode = %q, want bearer", body.AuthMode)
	}
	if body.ReadHeaderTimeoutSec != 15 || body.ReadTimeoutSec != 120 || body.IdleTimeoutSec != 120 {
		t.Errorf("timeout fields not reflected: read_header=%d read=%d idle=%d", body.ReadHeaderTimeoutSec, body.ReadTimeoutSec, body.IdleTimeoutSec)
	}
	if body.MaxHeaderBytes != 1<<20 || body.MaxDecodedAudioSeconds != 600 {
		t.Errorf("resource limit fields not reflected: max_header_bytes=%d max_decoded_audio_seconds=%d", body.MaxHeaderBytes, body.MaxDecodedAudioSeconds)
	}
	if !body.Features["catalog"] || !body.Features["vocabulary"] {
		t.Errorf("features not reflected: %+v", body.Features)
	}
	if body.Features["tts_direct"] {
		t.Errorf("tts_direct should be false by default")
	}
	if body.ReadyStatus != "ready" {
		t.Errorf("ready_status = %q, want ready", body.ReadyStatus)
	}
}

func TestConfig_NilReadyStatus_Unavailable(t *testing.T) {
	h := newHandler(nil)

	rec := httptest.NewRecorder()
	h.config(rec, httptest.NewRequest(http.MethodGet, "/v1/config", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		ReadyStatus string `json:"ready_status"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.ReadyStatus != "unavailable" {
		t.Errorf("ready_status = %q, want unavailable when provider is nil", body.ReadyStatus)
	}
}

func TestConfig_NonGET_MethodNotAllowed(t *testing.T) {
	h := newHandler(func() string { return "ready" })

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.config(rec, httptest.NewRequest(method, "/v1/config", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
		if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
			t.Errorf("%s: Allow header = %q, want GET", method, allow)
		}
	}
}
