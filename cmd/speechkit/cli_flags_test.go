package main

import (
	"testing"
)

func TestDisableTelemetryFlagOverridesConfig(t *testing.T) {
	t.Cleanup(resetUpdateChannel)
	// Precondition: updates are enabled (mirrors default + Task 3 init).
	configureUpdateChannel(true, "https://api.github.com/repos/kombifyio/SpeechKit/releases/latest", 6, "")
	if !updateEnabled {
		t.Fatalf("precondition: updateEnabled should be true after configureUpdateChannel(true,...)")
	}

	// Apply the flag override.
	disableTelemetry()

	if updateEnabled {
		t.Errorf("updateEnabled: want false after disableTelemetry, got true")
	}
}

func TestDisableTelemetryIsIdempotent(t *testing.T) {
	t.Cleanup(resetUpdateChannel)
	configureUpdateChannel(true, "https://api.github.com/repos/kombifyio/SpeechKit/releases/latest", 6, "")

	disableTelemetry()
	disableTelemetry()

	if updateEnabled {
		t.Errorf("updateEnabled: want false after double disableTelemetry, got true")
	}
}

func TestDisableTelemetryPreservesManifestURL(t *testing.T) {
	t.Cleanup(resetUpdateChannel)
	const customURL = "https://example.com/releases/latest"
	configureUpdateChannel(true, customURL, 6, "")

	disableTelemetry()

	// disableTelemetry passes the current releaseAPIURL so the URL is preserved.
	if releaseAPIURL != customURL {
		t.Errorf("releaseAPIURL: want %q preserved, got %q", customURL, releaseAPIURL)
	}
}
