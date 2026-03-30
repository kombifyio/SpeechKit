package features

import (
	"testing"

	"github.com/kombifyio/SpeechKit/internal/auth"
	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestDetect_LocalMode(t *testing.T) {
	state := &config.InstallState{Mode: config.InstallModeLocal}
	f := Detect(state)

	if f.CloudMode {
		t.Fatal("expected CloudMode=false for local mode")
	}
	if f.HasAuth {
		t.Fatal("expected HasAuth=false for local mode without provider")
	}
	if f.InstallMode != "local" {
		t.Fatalf("expected InstallMode='local', got %q", f.InstallMode)
	}
}

func TestDetect_CloudMode(t *testing.T) {
	state := &config.InstallState{Mode: config.InstallModeCloud}
	f := Detect(state)

	if !f.CloudMode {
		t.Fatal("expected CloudMode=true for cloud mode")
	}
	if f.InstallMode != "cloud" {
		t.Fatalf("expected InstallMode='cloud', got %q", f.InstallMode)
	}
}

func TestDetect_EmptyMode(t *testing.T) {
	state := &config.InstallState{Mode: ""}
	f := Detect(state)

	if f.CloudMode {
		t.Fatal("expected CloudMode=false for empty mode")
	}
	if f.InstallMode != "" {
		t.Fatalf("expected InstallMode='', got %q", f.InstallMode)
	}
}

func TestDetect_NoAuthProvider(t *testing.T) {
	// Ensure no auth provider is registered (OSS default).
	// auth package starts with nil provider; we rely on that default.
	_ = auth.GetAuthProvider() // reference auth to keep import

	state := &config.InstallState{Mode: config.InstallModeLocal}
	f := Detect(state)

	if f.HasAuth {
		t.Fatal("expected HasAuth=false when no auth provider is registered")
	}
	if f.LoggedIn {
		t.Fatal("expected LoggedIn=false when no auth provider is registered")
	}
}
