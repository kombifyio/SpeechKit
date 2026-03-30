package audio

import (
	"errors"
	"testing"
)

func TestDefaultBackendUsesWindowsCapture(t *testing.T) {
	if got := defaultBackend(); got != BackendWindowsWASAPIMalgo {
		t.Fatalf("defaultBackend() = %q, want %q", got, BackendWindowsWASAPIMalgo)
	}
}

func TestNormalizeConfigAppliesAudioDefaults(t *testing.T) {
	cfg := normalizeConfig(Config{})

	if cfg.SampleRate != SampleRate {
		t.Fatalf("SampleRate = %d, want %d", cfg.SampleRate, SampleRate)
	}
	if cfg.Channels != Channels {
		t.Fatalf("Channels = %d, want %d", cfg.Channels, Channels)
	}
	if cfg.FrameSizeMs != 32 {
		t.Fatalf("FrameSizeMs = %d, want 32", cfg.FrameSizeMs)
	}
	if cfg.Backend != defaultBackend() {
		t.Fatalf("Backend = %q, want %q", cfg.Backend, defaultBackend())
	}
}

func TestNewCapturerWithConfigRejectsUnknownBackend(t *testing.T) {
	_, err := Open(Config{Backend: Backend("unknown")})
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
	if !errors.Is(err, ErrUnsupportedBackend) {
		t.Fatalf("expected ErrUnsupportedBackend, got %v", err)
	}
}

func TestRegisterBackendRejectsDuplicateNames(t *testing.T) {
	name := Backend("test-duplicate")
	if err := RegisterBackend(name, func(Config) (Session, error) { return nil, nil }); err != nil {
		t.Fatalf("first RegisterBackend() failed: %v", err)
	}
	t.Cleanup(func() {
		unregisterBackendForTest(name)
	})

	err := RegisterBackend(name, func(Config) (Session, error) { return nil, nil })
	if err == nil {
		t.Fatal("expected duplicate registration error")
	}
}
