//go:build linux

package core

import (
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestActiveVoiceAgentModeSettingUsesDirectProviderProfiles(t *testing.T) {
	tests := []struct {
		provider  string
		model     string
		wantID    string
		wantModel string
	}{
		{provider: "google", wantID: "realtime.google.gemini-native-audio", wantModel: "gemini-3.1-flash-live-preview"},
		{provider: "google", model: "gemini-3.5-live-translate-preview", wantID: "realtime.google.gemini-live-translate", wantModel: "gemini-3.5-live-translate-preview"},
		{provider: "deepgram", wantID: "realtime.deepgram.voice-agent", wantModel: "flux-general-multi"},
		{provider: "assemblyai", wantID: "realtime.assemblyai.voice-agent", wantModel: "assemblyai-voice-agent"},
		{provider: "openai", wantID: "realtime.openai.gpt-realtime-2", wantModel: "gpt-realtime-2"},
		{provider: "assemblyai", model: "custom-agent", wantID: "realtime.assemblyai.voice-agent", wantModel: "custom-agent"},
	}
	for _, tt := range tests {
		t.Run(tt.provider+"/"+tt.model, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.VoiceAgent.Provider = tt.provider
			cfg.VoiceAgent.Model = tt.model
			got := activeVoiceAgentModeSetting(cfg)
			if got.ProviderKind != "direct_provider" {
				t.Fatalf("provider kind = %q, want direct_provider", got.ProviderKind)
			}
			if got.ProfileID != tt.wantID {
				t.Fatalf("profile id = %q, want %q", got.ProfileID, tt.wantID)
			}
			if got.Model != tt.wantModel {
				t.Fatalf("model = %q, want %q", got.Model, tt.wantModel)
			}
		})
	}
}

func TestActiveDictationModeSettingUsesCurrentOpenAIDefault(t *testing.T) {
	cfg := &config.Config{}
	got := activeDictationModeSetting(cfg)
	if got.ProviderKind != "direct_provider" {
		t.Fatalf("provider kind = %q, want direct_provider", got.ProviderKind)
	}
	if got.ProfileID != "stt.openai.gpt-4o-transcribe" {
		t.Fatalf("profile id = %q, want current OpenAI transcription profile", got.ProfileID)
	}
	if got.Model != "gpt-4o-transcribe" {
		t.Fatalf("model = %q, want gpt-4o-transcribe", got.Model)
	}
}

func TestServerModeSettingFromProviderUsesDictationModelVariants(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		model     string
		wantID    string
		wantModel string
	}{
		{
			name:      "openai mini transcription variant",
			provider:  "openai",
			model:     "gpt-4o-mini-transcribe",
			wantID:    "stt.openai.gpt-4o-transcribe",
			wantModel: "gpt-4o-mini-transcribe",
		},
		{
			name:      "groq quality transcription variant",
			provider:  "groq",
			model:     "whisper-large-v3",
			wantID:    "stt.groq.whisper-large-v3-turbo",
			wantModel: "whisper-large-v3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serverModeSettingFromProvider(tt.provider, "dictation", tt.model)
			if got.ProviderKind != "direct_provider" {
				t.Fatalf("provider kind = %q, want direct_provider", got.ProviderKind)
			}
			if got.ProfileID != tt.wantID {
				t.Fatalf("profile id = %q, want %q", got.ProfileID, tt.wantID)
			}
			if got.Model != tt.wantModel {
				t.Fatalf("model = %q, want %q", got.Model, tt.wantModel)
			}
		})
	}
}

func TestActiveServerCredentialSettingsIncludesVoiceAgentProviders(t *testing.T) {
	cfg := &config.Config{}
	cfg.Providers.Deepgram.Enabled = true
	cfg.Providers.Deepgram.APIKeyEnv = "DG_TEST_KEY"
	cfg.Providers.AssemblyAI.Enabled = true
	cfg.Providers.AssemblyAI.APIKeyEnv = "AAI_TEST_KEY"

	got := activeServerCredentialSettings(cfg)
	if got.Deepgram.Enabled == nil || !*got.Deepgram.Enabled || got.Deepgram.Env != "DG_TEST_KEY" {
		t.Fatalf("Deepgram credential = %#v", got.Deepgram)
	}
	if got.AssemblyAI.Enabled == nil || !*got.AssemblyAI.Enabled || got.AssemblyAI.Env != "AAI_TEST_KEY" {
		t.Fatalf("AssemblyAI credential = %#v", got.AssemblyAI)
	}
}
