package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/audio"
)

func TestAudioStartupDiagnosticClassifiesSentinels(t *testing.T) {
	cases := []struct {
		name         string
		err          error
		wantCategory string
		wantSubstr   string
	}{
		{"unsupported_backend", fmt.Errorf("wrap: %w", audio.ErrUnsupportedBackend), "unsupported_backend", "MinGW build"},
		{"backend_unavailable", fmt.Errorf("wrap: %w", audio.ErrBackendUnavailable), "backend_unavailable", "config.toml"},
		{"permission_denied", errors.New("malgo: permission denied opening default capture device"), "permission_denied", "Privacy"},
		{"access_denied", errors.New("malgo: access is denied (0x80070005)"), "permission_denied", "Privacy"},
		{"no_device", errors.New("malgo: no input device found"), "no_device", "Plug in a microphone"},
		{"no_default", errors.New("malgo: no default capture device"), "no_device", "Plug in a microphone"},
		{"device_busy", errors.New("malgo: device is busy"), "device_busy", "exclusive mode"},
		{"in_use", errors.New("malgo: capture endpoint already in use"), "device_busy", "exclusive mode"},
		{"unknown_fallback", errors.New("malgo: 0xdeadbeef obscure native error"), "unknown", "support-bundle"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			category, guidance := audioStartupDiagnostic(tc.err)
			if category != tc.wantCategory {
				t.Errorf("category = %q, want %q (err=%v)", category, tc.wantCategory, tc.err)
			}
			if !strings.Contains(guidance, tc.wantSubstr) {
				t.Errorf("guidance = %q, want substring %q (err=%v)", guidance, tc.wantSubstr, tc.err)
			}
		})
	}
}

func TestAudioStartupDiagnosticNilIsOk(t *testing.T) {
	category, guidance := audioStartupDiagnostic(nil)
	if category != "ok" {
		t.Errorf("category = %q, want ok", category)
	}
	if guidance != "" {
		t.Errorf("guidance = %q, want empty string", guidance)
	}
}
