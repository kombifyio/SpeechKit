package config

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestNormalizeMeetingGenerationKeepsKnownProvidersOnceWithoutThePrimary(t *testing.T) {
	cfg := &Config{Meeting: MeetingConfig{
		GenerationProvider: "foundry",
		FallbackProviders:  []string{"foundry", "local", "openai", "local", "github_copilot"},
	}}
	NormalizeMeetingGeneration(cfg)
	if want := []string{MeetingProviderLocal, MeetingProviderCopilot}; !reflect.DeepEqual(cfg.Meeting.FallbackProviders, want) {
		t.Fatalf("fallbacks = %#v, want %#v", cfg.Meeting.FallbackProviders, want)
	}

	cfg.Meeting.GenerationProvider = "openai"
	NormalizeMeetingGeneration(cfg)
	if cfg.Meeting.GenerationProvider != MeetingProviderLocal {
		t.Fatalf("unknown primary = %q, want the local route", cfg.Meeting.GenerationProvider)
	}
}

func TestFoundryTranscriptGrantRoundTrips(t *testing.T) {
	var meeting MeetingConfig
	if meeting.HasFoundryTranscriptGrant() {
		t.Fatal("a fresh config must not carry a Foundry grant")
	}
	meeting.GrantFoundryTranscripts(time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC))
	if !meeting.HasFoundryTranscriptGrant() {
		t.Fatal("grant not recorded")
	}
	meeting.FoundryTranscriptGrantVersion = MeetingFoundryTranscriptGrantVersion - 1
	if meeting.HasFoundryTranscriptGrant() {
		t.Fatal("a grant of an older version must not count")
	}
	meeting.GrantFoundryTranscripts(time.Now())
	meeting.RevokeFoundryTranscripts()
	if meeting.HasFoundryTranscriptGrant() {
		t.Fatal("revoked grant still counts")
	}
}

func TestDefaultMeetingGenerationIsOnDeviceWithoutFallback(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Meeting.ProviderOrder(); !reflect.DeepEqual(got, []string{MeetingProviderLocal}) {
		t.Fatalf("default provider order = %#v", got)
	}
}
