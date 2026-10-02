package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func loadMeetingConfig(t *testing.T, meeting string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[meeting]\n"+meeting), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestMeetingFallbackPolicyMigratesToFallbackProviders(t *testing.T) {
	cases := []struct {
		name          string
		file          string
		wantPrimary   string
		wantFallbacks []string
	}{
		{"copilot with local fallback", "generation_provider = \"github_copilot\"\nfallback_policy = \"allow_local_fallback\"\n", MeetingProviderCopilot, []string{MeetingProviderLocal}},
		{"copilot without fallback", "generation_provider = \"github_copilot\"\nfallback_policy = \"local_only\"\n", MeetingProviderCopilot, []string{}},
		{"local ignores a local fallback", "generation_provider = \"local\"\nfallback_policy = \"allow_local_fallback\"\n", MeetingProviderLocal, []string{}},
		{"explicit providers win", "generation_provider = \"github_copilot\"\nfallback_policy = \"allow_local_fallback\"\nfallback_providers = [\"foundry\"]\n", MeetingProviderCopilot, []string{MeetingProviderFoundry}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := loadMeetingConfig(t, tc.file)
			if cfg.Meeting.GenerationProvider != tc.wantPrimary {
				t.Fatalf("primary = %q, want %q", cfg.Meeting.GenerationProvider, tc.wantPrimary)
			}
			if !reflect.DeepEqual(cfg.Meeting.FallbackProviders, tc.wantFallbacks) {
				t.Fatalf("fallbacks = %#v, want %#v", cfg.Meeting.FallbackProviders, tc.wantFallbacks)
			}
			if cfg.Meeting.FallbackPolicy != "" {
				t.Fatalf("legacy fallback_policy kept: %q", cfg.Meeting.FallbackPolicy)
			}
		})
	}
}

func TestMigratedMeetingSettingsDropTheLegacyKeyOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[meeting]\ngeneration_provider = \"github_copilot\"\nfallback_policy = \"allow_local_fallback\"\ngeneration_model = \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path) // #nosec G304 -- test temp file
	if err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{"fallback_policy", "generation_model"} {
		if strings.Contains(string(written), removed) {
			t.Fatalf("saved config still carries %s", removed)
		}
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded.Meeting.FallbackProviders, []string{MeetingProviderLocal}) {
		t.Fatalf("fallbacks after a save = %#v", reloaded.Meeting.FallbackProviders)
	}
}

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
