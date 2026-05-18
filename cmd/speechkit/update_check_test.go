package main

import (
	"testing"
	"time"
)

func resetUpdateChannel() {
	updateEnabled = true
	releaseAPIURL = "https://api.github.com/repos/kombifyio/SpeechKit/releases/latest"
	updateCheckInterval = 6 * time.Hour
	signaturePinThumbprint = ""
}

func TestConfigureUpdateChannelDefaults(t *testing.T) {
	t.Cleanup(resetUpdateChannel)
	configureUpdateChannel(true, "https://example.com/latest", 0, "")
	if !updateEnabled {
		t.Errorf("updateEnabled: want true, got false")
	}
	if updateCheckInterval != 6*time.Hour {
		t.Errorf("updateCheckInterval: want 6h, got %s", updateCheckInterval)
	}
	if releaseAPIURL != "https://example.com/latest" {
		t.Errorf("releaseAPIURL: want example.com/latest, got %q", releaseAPIURL)
	}
}

func TestConfigureUpdateChannelDisabledMakesRefreshNoOp(t *testing.T) {
	t.Cleanup(resetUpdateChannel)
	configureUpdateChannel(false, "https://should-not-be-called.example.com", 6, "")
	if updateEnabled {
		t.Errorf("updateEnabled: want false, got true")
	}
	refreshLatestRelease()

	_, ok := cachedLatestRelease()
	if ok {
		t.Errorf("cachedLatestRelease: want ok=false when disabled, got ok=true")
	}
}

func TestConfigureUpdateChannelCustomInterval(t *testing.T) {
	t.Cleanup(resetUpdateChannel)
	configureUpdateChannel(true, "https://example.com/latest", 12, "")
	if updateCheckInterval != 12*time.Hour {
		t.Errorf("updateCheckInterval: want 12h, got %s", updateCheckInterval)
	}
}

func TestConfigureUpdateChannelRejectsInvalidURL(t *testing.T) {
	t.Cleanup(resetUpdateChannel)
	configureUpdateChannel(true, "ftp://example.com/wrong-scheme", 6, "")
	if updateEnabled {
		t.Errorf("updateEnabled: want false after netsec rejects URL, got true")
	}
}

func TestConfigureUpdateChannelStoresThumbprint(t *testing.T) {
	t.Cleanup(resetUpdateChannel)
	configureUpdateChannel(true, "https://example.com/latest", 6, "ABCDEF0123456789ABCDEF0123456789ABCDEF01")
	if signaturePinThumbprint != "ABCDEF0123456789ABCDEF0123456789ABCDEF01" {
		t.Errorf("signaturePinThumbprint: want ABCDEF0123456789ABCDEF0123456789ABCDEF01, got %q", signaturePinThumbprint)
	}
}

func TestConfigureUpdateChannelEmptyThumbprintIsNoOp(t *testing.T) {
	t.Cleanup(resetUpdateChannel)
	configureUpdateChannel(true, "https://example.com/latest", 6, "")
	if signaturePinThumbprint != "" {
		t.Errorf("signaturePinThumbprint: want empty thumbprint, got %q", signaturePinThumbprint)
	}
}
