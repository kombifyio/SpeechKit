package main

import (
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
)

// TestRunWakewordSelfTest_NilConfigReturnsError documents the precondition that
// the self-test handler refuses to run with no config. The actual recording
// path requires a real audio device and is exercised manually via the panel
// "Test microphone" button on the dev box; covering it in a unit test would
// require a malgo mock that doesn't ship today.
func TestRunWakewordSelfTest_NilConfigReturnsError(t *testing.T) {
	rep := runWakewordSelfTest(nil, nil, 100*time.Millisecond)
	if rep.OK {
		t.Fatalf("expected OK=false with nil config, got %+v", rep)
	}
	if !strings.Contains(rep.Error, "config") {
		t.Fatalf("expected error to mention config, got %q", rep.Error)
	}
}

// TestWakewordSelfTestAdviceCoversAllDeviceKinds checks the user-facing
// advice strings render correctly across the report variants the UI banner
// will show.
func TestWakewordSelfTestAdviceCoversAllDeviceKinds(t *testing.T) {
	for _, tc := range []struct {
		name           string
		rep            WakewordSelfTestReport
		wantSubstrings []string
	}{
		{
			name:           "no bytes captured surfaces silent-mic guidance",
			rep:            WakewordSelfTestReport{CapturedBytes: 0},
			wantSubstrings: []string{"silent", "privacy settings"},
		},
		{
			name: "default-fallback mentions ID mismatch",
			rep: WakewordSelfTestReport{
				CapturedBytes: 96000, PeakLevel: 0.3,
				DeviceKind: "default-fallback", ResolvedDName: "Mic A",
			},
			wantSubstrings: []string{"didn't match", "Mic A"},
		},
		{
			name: "very low level recommends mic gain",
			rep: WakewordSelfTestReport{
				CapturedBytes: 96000, PeakLevel: 0.005,
				DeviceKind: "requested", ResolvedDName: "Mic B",
			},
			wantSubstrings: []string{"very low", "input gain"},
		},
		{
			name: "good level mentions debug mode hint",
			rep: WakewordSelfTestReport{
				CapturedBytes: 96000, PeakLevel: 0.4, RMSLevel: 0.12,
				DeviceKind: "requested", ResolvedDName: "Mic C",
			},
			wantSubstrings: []string{"OK", "debug mode"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := wakewordSelfTestAdvice(tc.rep)
			for _, want := range tc.wantSubstrings {
				if !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
					t.Fatalf("advice %q missing %q", got, want)
				}
			}
		})
	}
}

// TestWakewordSelfTestReportUsesEffectiveThreshold checks that a non-nil cfg
// produces the same threshold the running sidecar would see, so the UI
// banner shows numbers that match the live runtime.
func TestWakewordSelfTestReportUsesEffectiveThreshold(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.Backend = config.WakewordBackendLiveKitOpenWakeWord
	cfg.Wakeword.PhraseID = "hey_kombify"

	// runWakewordSelfTest tries to open audio — in the test env without a
	// real mic this can either return ok=false with an Open error (CI) or
	// ok=true with zero bytes (dev box, mic muted). Either way the threshold
	// must reflect cfg.
	rep := runWakewordSelfTest(cfg, nil, 50*time.Millisecond)
	if rep.EffectiveThr != 0.5 {
		t.Fatalf("EffectiveThr = %.2f, want 0.5 (openWakeWord upstream default for unset Threshold)", rep.EffectiveThr)
	}
	if rep.Backend != config.WakewordBackendLiveKitOpenWakeWord {
		t.Fatalf("Backend = %q, want livekit_openwakeword", rep.Backend)
	}
}
