package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A mistyped channel name stops the load instead of silently recording
// something else.
func TestLoadRejectsAnUnknownMeetingCaptureChannel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[meeting]\ncapture_channels = [\"mic\", \"speakers\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted an unknown capture channel")
	}
}

// HKCU may only remove channels from a machine-pinned list: it can never add
// the system loopback back to a mic-only pin, and emptying the pin leaves the
// microphone alone, never the two-channel default.
func TestUserHiveCannotAddTheSystemChannelBackToAMicOnlyPin(t *testing.T) {
	for _, tc := range []struct {
		name string
		user []string
	}{
		{name: "adds system", user: []string{"mic", "system"}},
		{name: "empties the pin", user: []string{"system"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaults()
			cfg.Meeting.CaptureChannels = []string{"mic", "system"}

			AttachPolicy(cfg, mergePolicyHives(PolicyValues{MeetingCaptureChannels: []string{"mic"}}, PolicyValues{}, PolicyValues{MeetingCaptureChannels: tc.user}))

			if got := cfg.Meeting.ResolvedCaptureChannels(); !slices.Equal(got, []string{"mic"}) {
				t.Fatalf("capture channels = %v, want the microphone alone", got)
			}
		})
	}
}
