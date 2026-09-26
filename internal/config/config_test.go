package config

import (
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"os"
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("Load defaults: %v", err)
	}

	// Deliberately NOT "de". This assertion used to demand German, which made
	// the locale default self-healing: anyone who fixed the config got a red
	// test and reverted. SpeechKit never pins a language by default.
	if cfg.General.Language != stt.LanguageMulti {
		t.Errorf("default language = %q, want %q", cfg.General.Language, stt.LanguageMulti)
	}
	if cfg.General.Hotkey != "ctrl+win" {
		t.Errorf("default hotkey = %q, want %q", cfg.General.Hotkey, "ctrl+win")
	}
	if cfg.General.DictateHotkey != "ctrl+win" {
		t.Errorf("default dictate hotkey = %q, want %q", cfg.General.DictateHotkey, "ctrl+win")
	}
	if cfg.General.AssistHotkey != "win+alt" {
		t.Errorf("default assist hotkey = %q, want %q", cfg.General.AssistHotkey, "win+alt")
	}
	if cfg.General.VoiceAgentHotkey != "ctrl+shift" {
		t.Errorf("default voice agent hotkey = %q, want %q", cfg.General.VoiceAgentHotkey, "ctrl+shift")
	}
	if !cfg.General.DictateEnabled {
		t.Fatal("dictation should be enabled by default")
	}
	if cfg.General.AssistEnabled {
		t.Fatal("assist should be disabled by default")
	}
	if cfg.General.VoiceAgentEnabled {
		t.Fatal("voice agent should be disabled by default")
	}
	if cfg.General.AutoStopSilenceMs != DefaultDictationPauseMs {
		t.Errorf("default silence ms = %d, want %d", cfg.General.AutoStopSilenceMs, DefaultDictationPauseMs)
	}
	if cfg.General.DictateSilenceTimeoutSec != DefaultDictateSilenceTimeoutSec {
		t.Errorf("default dictate silence timeout = %d, want %d", cfg.General.DictateSilenceTimeoutSec, DefaultDictateSilenceTimeoutSec)
	}
	if cfg.General.DictationIntermediateSegmentMs != DefaultDictationIntermediateSegmentMs {
		t.Errorf("default dictation intermediate segment ms = %d, want %d", cfg.General.DictationIntermediateSegmentMs, DefaultDictationIntermediateSegmentMs)
	}
	if cfg.General.DictationProcessingMode != DictationProcessingModeAuto {
		t.Errorf("default dictation processing mode = %q, want %q", cfg.General.DictationProcessingMode, DictationProcessingModeAuto)
	}
	if cfg.General.DictationLiveCommit != DictationLiveCommitPassage {
		t.Errorf("default dictation live commit = %q, want %q", cfg.General.DictationLiveCommit, DictationLiveCommitPassage)
	}
	if cfg.Audio.InputSource != AudioInputSourceMicrophone {
		t.Errorf("default audio input source = %q, want %q", cfg.Audio.InputSource, AudioInputSourceMicrophone)
	}
	if cfg.General.AutoStartOnLaunch {
		t.Fatal("general dashboard auto-open should be disabled by default")
	}
	if cfg.General.StartAtLogin {
		t.Fatal("general start-at-login should be disabled by default")
	}
	if cfg.General.EagerWarmup {
		t.Fatal("general eager warmup should be disabled by default")
	}
	if !cfg.Local.Enabled {
		t.Error("local provider should be enabled by default")
	}
	if cfg.LocalLLM.Enabled {
		t.Error("built-in local LLM should be disabled by default")
	}
	if cfg.LocalLLM.BaseURL != "http://127.0.0.1:8082/v1" {
		t.Errorf("default local LLM base URL = %q", cfg.LocalLLM.BaseURL)
	}
	if cfg.LocalLLM.UtilityModel != DefaultLocalLLMModel || cfg.LocalLLM.AssistModel != DefaultLocalLLMModel {
		t.Errorf("default local LLM models = utility:%q assist:%q", cfg.LocalLLM.UtilityModel, cfg.LocalLLM.AssistModel)
	}
	if got, want := cfg.ModelSelection.Dictate.PrimaryProfileID, DefaultDictatePrimaryProfileID; got != want {
		t.Errorf("default dictate primary profile = %q, want %q", got, want)
	}
	if got, want := cfg.ModelSelection.Assist.PrimaryProfileID, DefaultAssistPrimaryProfileID; got != want {
		t.Errorf("default assist primary profile = %q, want %q", got, want)
	}
	if got, want := cfg.ModelSelection.VoiceAgent.PrimaryProfileID, DefaultVoiceAgentPrimaryProfileID; got != want {
		t.Errorf("default voice agent primary profile = %q, want %q", got, want)
	}
	for _, sel := range []struct {
		name string
		got  string
	}{
		{"dictate", cfg.ModelSelection.Dictate.ModeSource},
		{"assist", cfg.ModelSelection.Assist.ModeSource},
		{"voice_agent", cfg.ModelSelection.VoiceAgent.ModeSource},
	} {
		if sel.got != ModeSourceLocal {
			t.Errorf("default %s mode_source = %q, want %q", sel.name, sel.got, ModeSourceLocal)
		}
	}
	if cfg.ServerConnection.Enabled {
		t.Error("server connection should be disabled by default")
	}
	expectedServerURL := ""
	expectedServerAuthMode := ServerConnectionAuthModeBearer
	expectedServerTokenEnv := "SPEECHKIT_SERVER_TOKEN"
	if cfg.ServerConnection.URL != expectedServerURL {
		t.Errorf("default server URL = %q, want %q", cfg.ServerConnection.URL, expectedServerURL)
	}
	if cfg.ServerConnection.AuthMode != expectedServerAuthMode {
		t.Errorf("default server auth mode = %q, want %q", cfg.ServerConnection.AuthMode, expectedServerAuthMode)
	}
	if cfg.ServerConnection.BearerTokenEnv != expectedServerTokenEnv {
		t.Errorf("default server token env = %q, want %q", cfg.ServerConnection.BearerTokenEnv, expectedServerTokenEnv)
	}
	if !cfg.ServerConnection.FallbackToLocal {
		t.Error("server connection should fall back to local by default")
	}
	if cfg.ServerConnection.RequestTimeoutSec != 30 {
		t.Errorf("default server request timeout = %d, want 30", cfg.ServerConnection.RequestTimeoutSec)
	}
	if cfg.Server.AuthMode != "bearer" {
		t.Errorf("default server auth_mode = %q, want bearer", cfg.Server.AuthMode)
	}
	if cfg.Server.LiveKit.Enabled {
		t.Error("server LiveKit token minting should be disabled by default")
	}
	if cfg.Server.LiveKit.APIKeyEnv != "LIVEKIT_API_KEY" || cfg.Server.LiveKit.APISecretEnv != "LIVEKIT_API_SECRET" {
		t.Errorf("server LiveKit env names = key:%q secret:%q", cfg.Server.LiveKit.APIKeyEnv, cfg.Server.LiveKit.APISecretEnv)
	}
	if cfg.Server.LiveKit.TokenTTLSec != 600 || cfg.Server.LiveKit.RoomPrefix != "speechkit-va" {
		t.Errorf("server LiveKit token defaults = ttl:%d room_prefix:%q", cfg.Server.LiveKit.TokenTTLSec, cfg.Server.LiveKit.RoomPrefix)
	}
	if cfg.HuggingFace.Enabled {
		t.Error("default HuggingFace should stay disabled until explicitly enabled")
	}
	if cfg.HuggingFace.Model != "openai/whisper-large-v3-turbo" {
		t.Errorf("default HF model = %q", cfg.HuggingFace.Model)
	}
	if cfg.VoiceAgent.Model != "" {
		t.Errorf("default voice agent model = %q, want empty (no provider-specific default model)", cfg.VoiceAgent.Model)
	}
	if cfg.VoiceAgent.FallbackModel != "" {
		t.Errorf("default voice agent fallback model = %q, want empty (no provider-specific default model)", cfg.VoiceAgent.FallbackModel)
	}
	if cfg.ModelSelection.TTS.PrimaryProfileID != DefaultTTSPrimaryProfileID {
		t.Errorf("default TTS primary = %q, want %q", cfg.ModelSelection.TTS.PrimaryProfileID, DefaultTTSPrimaryProfileID)
	}
	if cfg.VoiceAgent.FrameworkPrompt != "" {
		t.Errorf("default voice agent framework prompt = %q, want empty", cfg.VoiceAgent.FrameworkPrompt)
	}
	if cfg.VoiceAgent.RefinementPrompt != "" {
		t.Errorf("default voice agent refinement prompt = %q, want empty", cfg.VoiceAgent.RefinementPrompt)
	}
	if cfg.VoiceAgent.AgentProfileID != voiceagentprofile.DefaultID {
		t.Errorf("default voice agent profile = %q, want %q", cfg.VoiceAgent.AgentProfileID, voiceagentprofile.DefaultID)
	}
	if cfg.Routing.PreferLocalUnderSeconds != 10 {
		t.Errorf("default prefer local = %f, want 10", cfg.Routing.PreferLocalUnderSeconds)
	}
	if cfg.Routing.Strategy != "local-only" {
		t.Errorf("default routing strategy = %q, want %q", cfg.Routing.Strategy, "local-only")
	}
	if !cfg.UI.OverlayEnabled {
		t.Error("overlay should be enabled by default")
	}
	if cfg.UI.Visualizer != "pill" {
		t.Errorf("visualizer = %q, want %q", cfg.UI.Visualizer, "pill")
	}
	if cfg.UI.Design != "default" {
		t.Errorf("design = %q, want %q", cfg.UI.Design, "default")
	}
	if cfg.UI.AssistOverlayMode != OverlayFeedbackModeSmallFeedback {
		t.Errorf("assist overlay mode = %q, want %q", cfg.UI.AssistOverlayMode, OverlayFeedbackModeSmallFeedback)
	}
	if cfg.UI.VoiceAgentOverlayMode != OverlayFeedbackModeSmallFeedback {
		t.Errorf("voice agent overlay mode = %q, want %q", cfg.UI.VoiceAgentOverlayMode, OverlayFeedbackModeSmallFeedback)
	}
	if !cfg.Store.SaveAudio {
		t.Error("store audio persistence should be enabled by default for local mode")
	}
	if !cfg.Feedback.SaveAudio {
		t.Error("legacy feedback audio persistence should stay aligned with store defaults")
	}
	if cfg.Store.AudioRetentionDays != 7 {
		t.Errorf("store audio retention days = %d, want 7", cfg.Store.AudioRetentionDays)
	}
	if cfg.Feedback.AudioRetentionDays != 7 {
		t.Errorf("legacy feedback audio retention days = %d, want 7", cfg.Feedback.AudioRetentionDays)
	}
	if cfg.Providers.Google.Region != "europe-west3" {
		t.Errorf("default Google region = %q, want europe-west3 (EU compliance default)", cfg.Providers.Google.Region)
	}
	if cfg.Providers.Deepgram.STTModel != "nova-3" {
		t.Errorf("default Deepgram STT model = %q, want nova-3", cfg.Providers.Deepgram.STTModel)
	}
	if !cfg.Providers.Deepgram.STTSmartFormat {
		t.Error("default Deepgram smart format should be enabled")
	}
	if !cfg.Providers.Deepgram.STTNumerals {
		t.Error("default Deepgram numerals should be enabled")
	}
	if !cfg.Providers.Deepgram.STTUseVocabularyKeyterms {
		t.Error("default Deepgram vocabulary keyterms should be enabled")
	}
	if cfg.Wakeword.Backend != WakewordBackendSherpaKWS {
		t.Errorf("default wake-word backend = %q, want %q", cfg.Wakeword.Backend, WakewordBackendSherpaKWS)
	}
	if cfg.HandsFree.TargetMode != HandsFreeTargetVoiceAgent {
		t.Errorf("default hands-free target = %q, want %q", cfg.HandsFree.TargetMode, HandsFreeTargetVoiceAgent)
	}
	if cfg.HandsFree.ActivationPhraseID != "hey_kubi" {
		t.Errorf("default hands-free phrase = %q, want hey_kubi", cfg.HandsFree.ActivationPhraseID)
	}
	if cfg.HandsFree.AutoEndSilenceCutoffSec != 10 {
		t.Errorf("default hands-free auto-end = %d, want 10", cfg.HandsFree.AutoEndSilenceCutoffSec)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[general]
language = "en"
hotkey = "ctrl+f5"

[huggingface]
enabled = false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.General.Language != "en" {
		t.Errorf("language = %q, want %q", cfg.General.Language, "en")
	}
	if cfg.General.Hotkey != "ctrl+f5" {
		t.Errorf("hotkey = %q, want %q", cfg.General.Hotkey, "ctrl+f5")
	}
	if cfg.HuggingFace.Enabled {
		t.Error("HuggingFace should be disabled")
	}
	// Defaults should still be present for unset fields
	if cfg.Local.Port != DefaultLocalSTTPort {
		t.Errorf("local port = %d, want %d (default)", cfg.Local.Port, DefaultLocalSTTPort)
	}
}
