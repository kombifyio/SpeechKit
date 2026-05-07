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

func TestRegisterBackendRejectsInvalidNamesAndNilFactory(t *testing.T) {
	for _, name := range []Backend{"", BackendAuto} {
		t.Run(string(name), func(t *testing.T) {
			err := RegisterBackend(name, func(Config) (Session, error) { return &fakeSession{}, nil })
			if !errors.Is(err, ErrUnsupportedBackend) {
				t.Fatalf("RegisterBackend(%q) error = %v, want ErrUnsupportedBackend", name, err)
			}
		})
	}

	err := RegisterBackend(Backend("nil-factory"), nil)
	if !errors.Is(err, ErrUnsupportedBackend) {
		t.Fatalf("RegisterBackend(nil factory) error = %v, want ErrUnsupportedBackend", err)
	}
}

func TestOpenRegisteredBackendReceivesNormalizedConfig(t *testing.T) {
	name := Backend("test-normalized")
	var got Config
	if err := RegisterBackend(name, func(cfg Config) (Session, error) {
		got = cfg
		return &fakeSession{}, nil
	}); err != nil {
		t.Fatalf("register backend: %v", err)
	}
	t.Cleanup(func() { unregisterBackendForTest(name) })

	session, err := Open(Config{Backend: name})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if session == nil {
		t.Fatal("Open() returned nil session")
	}
	if got.SampleRate != SampleRate || got.Channels != Channels || got.FrameSizeMs != 32 {
		t.Fatalf("normalized config = %+v", got)
	}
}

func TestOpenWrapsBackendInitializationError(t *testing.T) {
	name := Backend("test-init-error")
	initErr := errors.New("device unavailable")
	if err := RegisterBackend(name, func(Config) (Session, error) {
		return nil, initErr
	}); err != nil {
		t.Fatalf("register backend: %v", err)
	}
	t.Cleanup(func() { unregisterBackendForTest(name) })

	_, err := Open(Config{Backend: name})
	if !errors.Is(err, initErr) {
		t.Fatalf("Open() error = %v, want %v", err, initErr)
	}
	if errors.Is(err, ErrUnsupportedBackend) || errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("Open() wrapped init error as backend sentinel: %v", err)
	}
}

func TestOpenReturnsBackendSentinelErrorsUnwrapped(t *testing.T) {
	for _, sentinel := range []error{ErrUnsupportedBackend, ErrBackendUnavailable} {
		name := Backend("test-sentinel-" + sentinel.Error())
		if err := RegisterBackend(name, func(Config) (Session, error) {
			return nil, sentinel
		}); err != nil {
			t.Fatalf("register backend: %v", err)
		}
		t.Cleanup(func() { unregisterBackendForTest(name) })

		_, err := Open(Config{Backend: name})
		if !errors.Is(err, sentinel) {
			t.Fatalf("Open() error = %v, want %v", err, sentinel)
		}
	}
}

type fakeSession struct{}

func (fakeSession) Start() error                  { return nil }
func (fakeSession) Stop() ([]byte, error)         { return nil, nil }
func (fakeSession) IsRunning() bool               { return false }
func (fakeSession) Events() <-chan Event          { return nil }
func (fakeSession) SetLevelHandler(func(float64)) {}
func (fakeSession) SetPCMHandler(func([]byte))    {}
func (fakeSession) Close() error                  { return nil }
