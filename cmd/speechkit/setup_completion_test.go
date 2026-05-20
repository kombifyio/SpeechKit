package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestCompleteSetupRejectsMissingLocalSpeechModel(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	installTestWhisperBinary(t)

	cfg := defaultTestConfig()
	cfg.Local.Enabled = true
	cfg.Local.Model = config.DefaultLocalSTTModel
	cfg.Local.Port = 8080
	cfg.Local.GPU = "auto"
	installState := &config.InstallState{Mode: config.InstallModeLocal}

	mux := http.NewServeMux()
	registerAppRoutes(mux, filepath.Join(t.TempDir(), "config.toml"), cfg, &appState{}, installState)

	req := httptest.NewRequest(http.MethodPost, "/app/complete-setup", strings.NewReader(""))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if installState.SetupDone {
		t.Fatal("setup_done should remain false when the local speech model is missing")
	}
}

func TestCompleteSetupAcceptsBundledLocalSpeechModel(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	installTestWhisperBinary(t)
	localAppData := os.Getenv("LOCALAPPDATA")

	cfg := defaultTestConfig()
	cfg.Local.Enabled = true
	cfg.Local.Model = config.DefaultLocalSTTModel
	cfg.Local.Port = 8080
	cfg.Local.GPU = "auto"
	modelPath := filepath.Join(localAppData, "SpeechKit", "models", config.DefaultLocalSTTModel)
	writeValidWhisperModelFile(t, modelPath)
	installState := &config.InstallState{Mode: config.InstallModeLocal}

	mux := http.NewServeMux()
	registerAppRoutes(mux, filepath.Join(t.TempDir(), "config.toml"), cfg, &appState{}, installState)

	req := httptest.NewRequest(http.MethodPost, "/app/complete-setup", strings.NewReader(""))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !installState.SetupDone {
		t.Fatal("setup_done should be true when the local speech model is present")
	}
}
