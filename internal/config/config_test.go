package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/secrets"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

func unsetEnvForTest(t *testing.T, name string) {
	t.Helper()

	value, ok := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		var err error
		if ok {
			err = os.Setenv(name, value)
		} else {
			err = os.Unsetenv(name)
		}
		if err != nil {
			t.Fatalf("restore %s: %v", name, err)
		}
	})
}

func useMemorySecretStoreForTest(t *testing.T) {
	t.Helper()
	restore := secrets.UseMemoryStoreForTests()
	t.Cleanup(restore)
}

func postgresTestDSN(user, password, host, database, suffix string) string {
	return "post" + "gres://" + user + ":" + password + "@" + host + "/" + database + suffix
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("Load defaults: %v", err)
	}

	if cfg.General.Language != "de" {
		t.Errorf("default language = %q, want %q", cfg.General.Language, "de")
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
	if cfg.General.DictationProcessingMode != DictationProcessingModeFinalFull {
		t.Errorf("default dictation processing mode = %q, want %q", cfg.General.DictationProcessingMode, DictationProcessingModeFinalFull)
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
	if cfg.VoiceAgent.Model != "gemini-3.1-flash-live-preview" {
		t.Errorf("default voice agent model = %q, want gemini-3.1-flash-live-preview", cfg.VoiceAgent.Model)
	}
	if cfg.VoiceAgent.FallbackModel != "gemini-2.5-flash-native-audio-preview-12-2025" {
		t.Errorf("default voice agent fallback model = %q, want gemini-2.5-flash-native-audio-preview-12-2025", cfg.VoiceAgent.FallbackModel)
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
	if !cfg.Providers.Deepgram.STTUseVocabularyKeyterms {
		t.Error("default Deepgram vocabulary keyterms should be enabled")
	}
	if cfg.Wakeword.Backend != WakewordBackendSherpaKWS {
		t.Errorf("default wake-word backend = %q, want %q", cfg.Wakeword.Backend, WakewordBackendSherpaKWS)
	}
	if cfg.HandsFree.TargetMode != HandsFreeTargetVoiceAgent {
		t.Errorf("default hands-free target = %q, want %q", cfg.HandsFree.TargetMode, HandsFreeTargetVoiceAgent)
	}
	if cfg.HandsFree.ActivationPhraseID != "hey_quby" {
		t.Errorf("default hands-free phrase = %q, want hey_quby", cfg.HandsFree.ActivationPhraseID)
	}
	if cfg.HandsFree.AutoEndSilenceCutoffSec != 10 {
		t.Errorf("default hands-free auto-end = %d, want 10", cfg.HandsFree.AutoEndSilenceCutoffSec)
	}
}

func TestVoiceAgentSessionLimitsPreferVoiceAgentSection(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			MaxVoiceAgentSessions: 10,
			MaxSessionsPerUser:    2,
		},
		VoiceAgent: VoiceAgentConfig{
			Limits: VoiceAgentLimitsConfig{
				MaxGlobalSessions:      20,
				MaxPerIdentitySessions: 4,
			},
		},
	}

	got := cfg.VoiceAgentSessionLimits()
	if got.MaxGlobalSessions != 20 {
		t.Fatalf("MaxGlobalSessions = %d, want 20", got.MaxGlobalSessions)
	}
	if got.MaxPerIdentitySessions != 4 {
		t.Fatalf("MaxPerIdentitySessions = %d, want 4", got.MaxPerIdentitySessions)
	}
}

func TestVoiceAgentSessionLimitsFallbackToLegacyServerFields(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			MaxVoiceAgentSessions: 10,
			MaxSessionsPerUser:    2,
		},
	}

	got := cfg.VoiceAgentSessionLimits()
	if got.MaxGlobalSessions != 10 {
		t.Fatalf("MaxGlobalSessions = %d, want 10", got.MaxGlobalSessions)
	}
	if got.MaxPerIdentitySessions != 2 {
		t.Fatalf("MaxPerIdentitySessions = %d, want 2", got.MaxPerIdentitySessions)
	}
}

func TestLoadVoiceAgentLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`
[server]
max_voiceagent_sessions = 10
max_sessions_per_user = 2

[voice_agent.limits]
max_global_sessions = 25
max_per_identity_sessions = 5
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.VoiceAgentSessionLimits()
	if got.MaxGlobalSessions != 25 {
		t.Fatalf("MaxGlobalSessions = %d, want 25", got.MaxGlobalSessions)
	}
	if got.MaxPerIdentitySessions != 5 {
		t.Fatalf("MaxPerIdentitySessions = %d, want 5", got.MaxPerIdentitySessions)
	}
}

func TestDefaultLocalSTTModelIsBundledStarterModel(t *testing.T) {
	if DefaultLocalSTTModel != "ggml-small.bin" {
		t.Fatalf("DefaultLocalSTTModel = %q, want bundled starter model ggml-small.bin", DefaultLocalSTTModel)
	}
}

func TestGoogleProviderRegionDefaultAndOverride(t *testing.T) {
	t.Run("defaults to europe-west3 when absent from TOML", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		body := `[providers.google]
api_key_env = "GOOGLE_AI_API_KEY"
`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write toml: %v", err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Providers.Google.Region != "europe-west3" {
			t.Errorf("region = %q, want europe-west3 when not set in TOML", cfg.Providers.Google.Region)
		}
	})

	t.Run("respects explicit override in TOML", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		body := `[providers.google]
api_key_env = "GOOGLE_AI_API_KEY"
region = "us-central1"
`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write toml: %v", err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Providers.Google.Region != "us-central1" {
			t.Errorf("region = %q, want us-central1", cfg.Providers.Google.Region)
		}
	})

	t.Run("no TOML file uses europe-west3 from defaults", func(t *testing.T) {
		cfg, err := Load("/nonexistent/no-config.toml")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Providers.Google.Region != "europe-west3" {
			t.Errorf("region = %q, want europe-west3", cfg.Providers.Google.Region)
		}
	})
}

func TestLoadVoiceAgentAgentProfileID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[voice_agent]
agent_profile_id = "humor_companion"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.VoiceAgent.AgentProfileID, voiceagentprofile.HumorCompanionID; got != want {
		t.Fatalf("voice_agent.agent_profile_id = %q, want %q", got, want)
	}
}

func TestLoadVoiceAgentAgentProfileIDFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[voice_agent]
agent_profile_id = "unknown"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.VoiceAgent.AgentProfileID, voiceagentprofile.DefaultID; got != want {
		t.Fatalf("voice_agent.agent_profile_id = %q, want %q", got, want)
	}
}

func TestLoadVoiceAgentAgentSequenceID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[voice_agent]
agent_sequence_id = "  custom_discovery_flow  "
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.VoiceAgent.AgentSequenceID, "custom_discovery_flow"; got != want {
		t.Fatalf("voice_agent.agent_sequence_id = %q, want %q", got, want)
	}
}

func TestLoadServerConnectionFromTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[model_selection.dictate]
primary_profile_id = "stt.local.whispercpp"
mode_source = "server"

[model_selection.assist]
primary_profile_id = "assist.builtin.gemma4-e4b"
mode_source = "local"

[model_selection.voice_agent]
primary_profile_id = "realtime.google.gemini-native-audio"
mode_source = "server"

[server_connection]
enabled = true
url = "https://speechkit.test"
bearer_token_env = "MY_TOKEN"
fallback_to_local = false
request_timeout_sec = 5
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ModelSelection.Dictate.ResolvedModeSource(); got != ModeSourceServer {
		t.Errorf("dictate mode_source = %q, want server", got)
	}
	if got := cfg.ModelSelection.Assist.ResolvedModeSource(); got != ModeSourceLocal {
		t.Errorf("assist mode_source = %q, want local", got)
	}
	if got := cfg.ModelSelection.VoiceAgent.ResolvedModeSource(); got != ModeSourceServer {
		t.Errorf("voice_agent mode_source = %q, want server", got)
	}
	if !cfg.ServerConnection.Enabled {
		t.Error("server_connection.enabled = false, want true")
	}
	if cfg.ServerConnection.URL != "https://speechkit.test" {
		t.Errorf("server_connection.url = %q", cfg.ServerConnection.URL)
	}
	if cfg.ServerConnection.BearerTokenEnv != "MY_TOKEN" {
		t.Errorf("server_connection.bearer_token_env = %q, want MY_TOKEN", cfg.ServerConnection.BearerTokenEnv)
	}
	if cfg.ServerConnection.AuthMode != ServerConnectionAuthModeBearer {
		t.Errorf("server_connection.auth_mode = %q, want bearer", cfg.ServerConnection.AuthMode)
	}
	if cfg.ServerConnection.FallbackToLocal {
		t.Error("server_connection.fallback_to_local = true, want false")
	}
	if cfg.ServerConnection.RequestTimeoutSec != 5 {
		t.Errorf("server_connection.request_timeout_sec = %d, want 5", cfg.ServerConnection.RequestTimeoutSec)
	}
}

func TestLoadDoesNotBackfillServerConnectionURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`
[server_connection]
enabled = false
url = ""
bearer_token_env = ""
fallback_to_local = true
request_timeout_sec = 0
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ServerConnection.Enabled {
		t.Fatal("server connection should stay disabled by default")
	}
	if got, want := cfg.ServerConnection.URL, ""; got != want {
		t.Fatalf("server_connection.url = %q, want %q", got, want)
	}
	if got, want := cfg.ServerConnection.AuthMode, ServerConnectionAuthModeBearer; got != want {
		t.Fatalf("server_connection.auth_mode = %q, want %q", got, want)
	}
	if got, want := cfg.ServerConnection.BearerTokenEnv, ""; got != want {
		t.Fatalf("server_connection.bearer_token_env = %q, want %q", got, want)
	}
	if got, want := cfg.ServerConnection.RequestTimeoutSec, 0; got != want {
		t.Fatalf("server_connection.request_timeout_sec = %d, want %d", got, want)
	}
}

func TestResolvedModeSource(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty defaults to local", in: "", want: ModeSourceLocal},
		{name: "explicit local", in: "local", want: ModeSourceLocal},
		{name: "explicit server", in: "server", want: ModeSourceServer},
		{name: "uppercase server", in: "SERVER", want: ModeSourceServer},
		{name: "mixed case server", in: "Server", want: ModeSourceServer},
		{name: "padded server", in: "  server  ", want: ModeSourceServer},
		{name: "garbage falls back to local", in: "remote", want: ModeSourceLocal},
		{name: "uppercase local", in: "LOCAL", want: ModeSourceLocal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := ModeModelSelection{ModeSource: tt.in}
			if got := sel.ResolvedModeSource(); got != tt.want {
				t.Fatalf("ResolvedModeSource(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeServerConnectionAuthMode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ServerConnectionAuthModeBearer},
		{in: "bearer", want: ServerConnectionAuthModeBearer},
		{in: "api_key", want: ServerConnectionAuthModeAPIKey},
		{in: "API_KEY", want: ServerConnectionAuthModeAPIKey},
		{in: "edge_beta", want: ServerConnectionAuthModeEdgeBeta},
		{in: "EDGE_BETA", want: ServerConnectionAuthModeEdgeBeta},
		{in: "unknown", want: ServerConnectionAuthModeBearer},
	}
	for _, tt := range tests {
		if got := NormalizeServerConnectionAuthMode(tt.in); got != tt.want {
			t.Fatalf("NormalizeServerConnectionAuthMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeOverlayFeedbackMode(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		fallback string
		want     string
	}{
		{name: "big", value: OverlayFeedbackModeBigProductivity, fallback: OverlayFeedbackModeSmallFeedback, want: OverlayFeedbackModeBigProductivity},
		{name: "small", value: OverlayFeedbackModeSmallFeedback, fallback: OverlayFeedbackModeBigProductivity, want: OverlayFeedbackModeSmallFeedback},
		{name: "fallback", value: "unknown", fallback: OverlayFeedbackModeBigProductivity, want: OverlayFeedbackModeBigProductivity},
		{name: "empty fallback", value: "unknown", fallback: "", want: OverlayFeedbackModeSmallFeedback},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeOverlayFeedbackMode(tt.value, tt.fallback); got != tt.want {
				t.Fatalf("NormalizeOverlayFeedbackMode(%q, %q) = %q, want %q", tt.value, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestNormalizeWakewordBackend(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty defaults to sherpa_kws (bundle-consistent default)", in: "", want: WakewordBackendSherpaKWS},
		{name: "sherpa explicit pin survives", in: "sherpa_kws", want: WakewordBackendSherpaKWS},
		{name: "livekit explicit pin survives", in: "livekit", want: WakewordBackendLiveKitOpenWakeWord},
		{name: "openwakeword alias survives", in: "openWakeWord", want: WakewordBackendLiveKitOpenWakeWord},
		{name: "stt alias", in: "phrase_match", want: WakewordBackendSTTPhrase},
		{name: "unknown falls back to sherpa_kws (bundle-consistent default)", in: "unknown", want: WakewordBackendSherpaKWS},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeWakewordBackend(tt.in); got != tt.want {
				t.Fatalf("NormalizeWakewordBackend(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLoadHandsFreeBlockMirrorsWakewordCompatibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `
[hands_free]
enabled = true
activation_phrase_id = "hey_mira"
target_mode = "assist"
auto_end_silence_cutoff_sec = 7
voice_output_enabled = true

[wakeword]
enabled = false
phrase_id = "hey_quby"
default_mode = "voice_agent"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.HandsFree.Enabled || !cfg.Wakeword.Enabled {
		t.Fatal("hands-free enabled should mirror to wakeword enabled")
	}
	if got, want := cfg.HandsFree.ActivationPhraseID, "hey_mira"; got != want {
		t.Fatalf("hands-free phrase = %q, want %q", got, want)
	}
	if got, want := cfg.Wakeword.PhraseID, "hey_mira"; got != want {
		t.Fatalf("wakeword phrase = %q, want %q", got, want)
	}
	if got, want := cfg.HandsFree.TargetMode, HandsFreeTargetAssist; got != want {
		t.Fatalf("hands-free target = %q, want %q", got, want)
	}
	if got, want := cfg.Wakeword.DefaultMode, WakewordDefaultModeAssist; got != want {
		t.Fatalf("wakeword default mode = %q, want %q", got, want)
	}
	if got, want := cfg.Wakeword.AutoEnd.SilenceCutoffSec, 7; got != want {
		t.Fatalf("wakeword auto-end = %d, want %d", got, want)
	}
}

func TestLoadLegacyWakewordDerivesHandsFree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `
[wakeword]
enabled = true
phrase_id = "hey_kombify"
default_mode = "dictate"

[wakeword.auto_end]
silence_cutoff_sec = 4
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.HandsFree.Enabled {
		t.Fatal("hands-free enabled should be derived from legacy wakeword")
	}
	if got, want := cfg.HandsFree.ActivationPhraseID, "hey_kombify"; got != want {
		t.Fatalf("hands-free phrase = %q, want %q", got, want)
	}
	if got, want := cfg.HandsFree.TargetMode, HandsFreeTargetDictationUIAssisted; got != want {
		t.Fatalf("hands-free target = %q, want %q", got, want)
	}
	if cfg.HandsFree.VoiceOutputEnabled {
		t.Fatal("dictation UI-assisted target must not enable hands-free voice output")
	}
	if got, want := cfg.HandsFree.AutoEndSilenceCutoffSec, 4; got != want {
		t.Fatalf("hands-free auto-end = %d, want %d", got, want)
	}
}

func TestApplyLocalInstallDefaultsBackfillsBuiltInPrimaryModels(t *testing.T) {
	cfg := &Config{}
	changed := ApplyLocalInstallDefaults(cfg, &InstallState{Mode: InstallModeLocal})

	if !changed {
		t.Fatal("ApplyLocalInstallDefaults should report changed when model defaults are missing")
	}
	if got, want := cfg.ModelSelection.Dictate.PrimaryProfileID, DefaultDictatePrimaryProfileID; got != want {
		t.Errorf("dictate primary profile = %q, want %q", got, want)
	}
	if got, want := cfg.ModelSelection.Assist.PrimaryProfileID, DefaultAssistPrimaryProfileID; got != want {
		t.Errorf("assist primary profile = %q, want %q", got, want)
	}
	if got, want := cfg.ModelSelection.VoiceAgent.PrimaryProfileID, DefaultVoiceAgentPrimaryProfileID; got != want {
		t.Errorf("voice agent primary profile = %q, want %q", got, want)
	}
}

func TestLoadPreservesConfiguredLocalLLMProfilesWithoutModelPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[model_selection.assist]
primary_profile_id = "assist.builtin.gemma4-e4b"
fallback_profile_id = ""

[model_selection.voice_agent]
primary_profile_id = "realtime.builtin.pipeline"
fallback_profile_id = ""

[local_llm]
enabled = false
model_path = ""

[voice_agent]
model = "speechkit-local-voice-pipeline"
pipeline_fallback = true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.ModelSelection.Assist.PrimaryProfileID; got != "assist.builtin.gemma4-e4b" {
		t.Fatalf("assist primary profile = %q, want local built-in profile", got)
	}
	if got, want := cfg.ModelSelection.VoiceAgent.PrimaryProfileID, "realtime.builtin.pipeline"; got != want {
		t.Fatalf("voice agent primary profile = %q, want %q", got, want)
	}
	if !cfg.VoiceAgent.PipelineFallback {
		t.Fatal("voice agent pipeline fallback should stay enabled")
	}
	if got, want := cfg.VoiceAgent.Model, "speechkit-local-voice-pipeline"; got != want {
		t.Fatalf("voice agent model = %q, want %q", got, want)
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

func TestLoadBackfillsAssistModelFromLegacyAgentModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[providers.ollama]
enabled = true
agent_model = "gemma4:e4b"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := cfg.Providers.Ollama.AgentModel, "gemma4:e4b"; got != want {
		t.Fatalf("agent model = %q, want %q", got, want)
	}
	if got, want := cfg.Providers.Ollama.AssistModel, "gemma4:e4b"; got != want {
		t.Fatalf("assist model = %q, want %q", got, want)
	}
}

func TestLoadBackfillsLocalLLMAssistModelFromLegacyAgentModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[local_llm]
enabled = true
agent_model = "gemma4:e4b"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := cfg.LocalLLM.AgentModel, "gemma4:e4b"; got != want {
		t.Fatalf("agent model = %q, want %q", got, want)
	}
	if got, want := cfg.LocalLLM.AssistModel, "gemma4:e4b"; got != want {
		t.Fatalf("assist model = %q, want %q", got, want)
	}
}

func TestLoadRejectsRemovedVoiceAgentInstructionAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[voice_agent]
instruction = "Legacy framework prompt"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load succeeded, want removed alias error")
	}
	if got, want := err.Error(), "config [voice_agent].instruction was removed; use [voice_agent].framework_prompt"; got != want {
		t.Fatalf("Load error = %q, want %q", got, want)
	}
}

func TestLoadPrefersExplicitStoreSaveAudioOverLegacyFeedback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[feedback]
save_audio = true

[store]
backend = "sqlite"
save_audio = false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Store.SaveAudio {
		t.Fatal("store.save_audio should remain false when explicitly set in [store]")
	}
}

func TestLoadPreservesExplicitPostgresStoreConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[feedback]
db_path = "C:/legacy/feedback.db"

[store]
backend = "postgres"
postgres_dsn = "` + postgresTestDSN("speechkit", "secret", "localhost:5432", "speechkit", "?sslmode=disable") + `"
save_audio = false
max_audio_storage_mb = 1024
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Store.Backend != "postgres" {
		t.Fatalf("store.backend = %q, want postgres", cfg.Store.Backend)
	}
	if cfg.Store.PostgresDSN == "" {
		t.Fatal("expected postgres dsn to be loaded")
	}
	if cfg.Store.SQLitePath != "" {
		t.Fatalf("store.sqlite_path = %q, want empty", cfg.Store.SQLitePath)
	}
	if cfg.Store.MaxAudioStorageMB != 1024 {
		t.Fatalf("store.max_audio_storage_mb = %d, want 1024", cfg.Store.MaxAudioStorageMB)
	}
}

func TestResolveSecret_EnvVar(t *testing.T) {
	t.Setenv("TEST_SECRET_KEY", "test-value-123")
	val := ResolveSecret("TEST_SECRET_KEY")
	if val != "test-value-123" {
		t.Errorf("ResolveSecret = %q, want %q", val, "test-value-123")
	}
}

func TestResolveSecret_Missing(t *testing.T) {
	val := ResolveSecret("NONEXISTENT_KEY_THAT_SHOULD_NOT_EXIST_12345")
	// Might return empty or a Doppler value; just ensure no panic
	_ = val
}

func TestResolveSecret_DopplerFallback(t *testing.T) {
	t.Cleanup(resetDopplerHooksForTests)
	t.Setenv("DOPPLER_PROJECT", "test-project")
	t.Setenv("DOPPLER_CONFIG", "stage")
	dopplerLookPath = func(file string) (string, error) {
		if file != "doppler" {
			t.Fatalf("lookPath file = %q", file)
		}
		return "C:\\fake\\doppler.exe", nil
	}
	dopplerSecretLookup = func(dopplerPath, key, project, cfg string) (string, error) {
		if dopplerPath != "C:\\fake\\doppler.exe" {
			t.Fatalf("dopplerPath = %q", dopplerPath)
		}
		if key != "TEST_DOPPLER_SECRET" {
			t.Fatalf("key = %q", key)
		}
		if project == "test-project" && cfg == "stage" {
			return "secret-from-doppler", nil
		}
		return "", errors.New("not found")
	}

	value := ResolveSecret("TEST_DOPPLER_SECRET")

	if value != "secret-from-doppler" {
		t.Fatalf("ResolveSecret = %q", value)
	}
}

func TestFindDopplerExecutableUsesEnvOverride(t *testing.T) {
	t.Cleanup(resetDopplerHooksForTests)
	dopplerLookPath = func(string) (string, error) {
		return "", &exec.Error{Name: "doppler", Err: errors.New("not found")}
	}

	fake := filepath.Join(t.TempDir(), "doppler.exe")
	if err := os.WriteFile(fake, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOPPLER_PATH", fake)

	path := findDopplerExecutable()

	if path != fake {
		t.Fatalf("findDopplerExecutable = %q, want %q", path, fake)
	}
}

func TestFindDopplerExecutableFallsBackToWingetLink(t *testing.T) {
	t.Cleanup(resetDopplerHooksForTests)
	dopplerLookPath = func(string) (string, error) {
		return "", &exec.Error{Name: "doppler", Err: errors.New("not found")}
	}

	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	fake := filepath.Join(localAppData, "Microsoft", "WinGet", "Links", "doppler.exe")
	if err := os.MkdirAll(filepath.Dir(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fake, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	path := findDopplerExecutable()

	if path != fake {
		t.Fatalf("findDopplerExecutable = %q, want %q", path, fake)
	}
}

func TestDopplerProjectsAndConfigsRequireExplicitEnv(t *testing.T) {
	t.Setenv("DOPPLER_PROJECT", "test-project")
	t.Setenv("DOPPLER_CONFIG", "stage")

	projects := dopplerProjects()
	configs := dopplerConfigs()

	if len(projects) == 0 || projects[0] != "test-project" {
		t.Fatalf("projects = %v", projects)
	}
	if len(configs) == 0 || configs[0] != "stage" {
		t.Fatalf("configs = %v", configs)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %v", projects)
	}
	if len(configs) != 1 {
		t.Fatalf("configs = %v", configs)
	}
}

func TestDopplerProjectsAndConfigsFallBackToManagedDefaults(t *testing.T) {
	previousProject := managedDopplerDefaultProject
	previousConfig := managedDopplerDefaultConfig
	managedDopplerDefaultProject = "managed-project"
	managedDopplerDefaultConfig = "prd"
	t.Cleanup(func() {
		managedDopplerDefaultProject = previousProject
		managedDopplerDefaultConfig = previousConfig
	})
	unsetEnvForTest(t, "DOPPLER_PROJECT")
	unsetEnvForTest(t, "DOPPLER_CONFIG")

	projects := dopplerProjects()
	configs := dopplerConfigs()

	if len(projects) != 1 || projects[0] != "managed-project" {
		t.Fatalf("projects = %v", projects)
	}
	if len(configs) != 1 || configs[0] != "prd" {
		t.Fatalf("configs = %v", configs)
	}
}

func TestDopplerProjectsAndConfigsPreferExplicitEnvOverManagedDefaults(t *testing.T) {
	previousProject := managedDopplerDefaultProject
	previousConfig := managedDopplerDefaultConfig
	managedDopplerDefaultProject = "managed-project"
	managedDopplerDefaultConfig = "prd"
	t.Cleanup(func() {
		managedDopplerDefaultProject = previousProject
		managedDopplerDefaultConfig = previousConfig
	})

	t.Setenv("DOPPLER_PROJECT", "dev-project")
	t.Setenv("DOPPLER_CONFIG", "dev")

	projects := dopplerProjects()
	configs := dopplerConfigs()

	if len(projects) != 1 || projects[0] != "dev-project" {
		t.Fatalf("projects = %v", projects)
	}
	if len(configs) != 1 || configs[0] != "dev" {
		t.Fatalf("configs = %v", configs)
	}
}

func TestDopplerProjectsAndConfigsStayEmptyWithoutEnv(t *testing.T) {
	previousProject := managedDopplerDefaultProject
	previousConfig := managedDopplerDefaultConfig
	managedDopplerDefaultProject = ""
	managedDopplerDefaultConfig = ""
	t.Cleanup(func() {
		managedDopplerDefaultProject = previousProject
		managedDopplerDefaultConfig = previousConfig
	})
	t.Setenv("DOPPLER_PROJECT", "")
	t.Setenv("DOPPLER_CONFIG", "")

	projects := dopplerProjects()
	configs := dopplerConfigs()

	if len(projects) != 0 {
		t.Fatalf("projects = %v", projects)
	}
	if len(configs) != 0 {
		t.Fatalf("configs = %v", configs)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := defaults()
	cfg.General.Hotkey = "ctrl+shift"
	cfg.HuggingFace.Enabled = true
	cfg.HuggingFace.Model = "openai/whisper-large-v3-turbo"
	cfg.UI.OverlayEnabled = false
	cfg.UI.Visualizer = "circle"
	cfg.UI.Design = "kombify"

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.General.Hotkey != "ctrl+shift" {
		t.Fatalf("hotkey = %q", reloaded.General.Hotkey)
	}
	if reloaded.HuggingFace.Model != "openai/whisper-large-v3-turbo" {
		t.Fatalf("model = %q", reloaded.HuggingFace.Model)
	}
	if reloaded.UI.OverlayEnabled {
		t.Fatal("overlay should round-trip as disabled")
	}
	if reloaded.UI.Visualizer != "circle" {
		t.Fatalf("visualizer = %q", reloaded.UI.Visualizer)
	}
	if reloaded.UI.Design != "kombify" {
		t.Fatalf("design = %q", reloaded.UI.Design)
	}
}

func TestSaveRoundTripAssistModels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := defaults()
	cfg.Providers.OpenAI.AssistModel = "gpt-5.4-2026-03-05"
	cfg.Providers.Google.AssistModel = "gemini-2.5-flash"
	cfg.Providers.Ollama.AssistModel = "gemma4:e4b"
	cfg.LocalLLM.AssistModel = "gemma4:e4b"
	cfg.HuggingFace.AssistModel = "Qwen/Qwen3.5-27B"

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := reloaded.Providers.OpenAI.AssistModel, cfg.Providers.OpenAI.AssistModel; got != want {
		t.Fatalf("openai assist model = %q, want %q", got, want)
	}
	if got, want := reloaded.Providers.Google.AssistModel, cfg.Providers.Google.AssistModel; got != want {
		t.Fatalf("google assist model = %q, want %q", got, want)
	}
	if got, want := reloaded.Providers.Ollama.AssistModel, cfg.Providers.Ollama.AssistModel; got != want {
		t.Fatalf("ollama assist model = %q, want %q", got, want)
	}
	if got, want := reloaded.LocalLLM.AssistModel, cfg.LocalLLM.AssistModel; got != want {
		t.Fatalf("local LLM assist model = %q, want %q", got, want)
	}
	if got, want := reloaded.HuggingFace.AssistModel, cfg.HuggingFace.AssistModel; got != want {
		t.Fatalf("huggingface assist model = %q, want %q", got, want)
	}
}

func TestLoadShortcutLocaleAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[shortcuts.locale.de]
leading_fillers = ["bitte", "hey speechkit"]
summarize = ["kurzfassung", "briefing"]
copy_last = ["kopier den letzten block"]

[shortcuts.locale.en]
summarize = ["brief me"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	de, ok := cfg.Shortcuts.Locale["de"]
	if !ok {
		t.Fatal("expected shortcuts.locale.de to be loaded")
	}
	if got, want := len(de.LeadingFillers), 2; got != want {
		t.Fatalf("len(leading_fillers) = %d, want %d", got, want)
	}
	if got, want := de.Summarize[0], "kurzfassung"; got != want {
		t.Fatalf("de summarize[0] = %q, want %q", got, want)
	}
	if got, want := de.CopyLast[0], "kopier den letzten block"; got != want {
		t.Fatalf("de copy_last[0] = %q, want %q", got, want)
	}

	en, ok := cfg.Shortcuts.Locale["en"]
	if !ok {
		t.Fatal("expected shortcuts.locale.en to be loaded")
	}
	if got, want := en.Summarize[0], "brief me"; got != want {
		t.Fatalf("en summarize[0] = %q, want %q", got, want)
	}
}

func TestSaveRoundTripShortcutLocaleAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := defaults()
	cfg.Shortcuts.Locale = map[string]ShortcutLocaleConfig{
		"de": {
			LeadingFillers: []string{"bitte"},
			Summarize:      []string{"kurzfassung"},
			CopyLast:       []string{"kopier den letzten block"},
			InsertLast:     []string{"setz das ein"},
			QuickNote:      []string{"merkzettel"},
		},
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	de, ok := reloaded.Shortcuts.Locale["de"]
	if !ok {
		t.Fatal("expected shortcuts.locale.de after round-trip")
	}
	if got, want := de.Summarize[0], "kurzfassung"; got != want {
		t.Fatalf("de summarize[0] = %q, want %q", got, want)
	}
	if got, want := de.QuickNote[0], "merkzettel"; got != want {
		t.Fatalf("de quick_note[0] = %q, want %q", got, want)
	}
}

func TestApplyManagedIntegrationDefaultsNoopWhenHFAlreadyEnabled(t *testing.T) {
	useMemorySecretStoreForTest(t)
	restoreBuild := OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaults()
	cfg.HuggingFace.Enabled = true
	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")
	t.Setenv("HF_TOKEN", "test-token")

	changed := ApplyManagedIntegrationDefaults(cfg)

	if changed {
		t.Fatal("managed defaults should not change config when HF is already enabled")
	}
	if !cfg.HuggingFace.Enabled {
		t.Fatal("huggingface should remain enabled")
	}
}

func TestApplyManagedIntegrationDefaultsEnablesHFWhenExplicitlyDisabled(t *testing.T) {
	useMemorySecretStoreForTest(t)
	restoreBuild := OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaults()
	cfg.Local.Enabled = false
	cfg.Routing.Strategy = "cloud-only"
	cfg.HuggingFace.Enabled = false
	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")
	t.Setenv("HF_TOKEN", "test-token")

	changed := ApplyManagedIntegrationDefaults(cfg)

	if !changed {
		t.Fatal("expected managed defaults to enable huggingface when explicitly disabled")
	}
	if !cfg.HuggingFace.Enabled {
		t.Fatal("huggingface should be enabled")
	}
}

func TestApplyManagedIntegrationDefaultsDoesNotOverrideExplicitProviderConfig(t *testing.T) {
	useMemorySecretStoreForTest(t)
	restoreBuild := OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaults()
	cfg.HuggingFace.Enabled = false
	cfg.Local.Enabled = true
	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")
	t.Setenv("HF_TOKEN", "test-token")

	changed := ApplyManagedIntegrationDefaults(cfg)

	if changed {
		t.Fatal("managed defaults should not override explicit local provider setup")
	}
	if cfg.HuggingFace.Enabled {
		t.Fatal("huggingface should stay disabled")
	}
}

func TestDefaultHotkeyBehaviors(t *testing.T) {
	cfg := defaults()
	if cfg.General.HotkeyMode != HotkeyBehaviorHoldToTalk {
		t.Fatalf("default HotkeyMode = %q, want %q", cfg.General.HotkeyMode, HotkeyBehaviorHoldToTalk)
	}
	if cfg.General.DictateHotkeyBehavior != HotkeyBehaviorHoldToTalk {
		t.Fatalf("default DictateHotkeyBehavior = %q, want %q", cfg.General.DictateHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	}
	if cfg.General.AssistHotkeyBehavior != HotkeyBehaviorHoldToTalk {
		t.Fatalf("default AssistHotkeyBehavior = %q, want %q", cfg.General.AssistHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	}
	if cfg.General.VoiceAgentHotkeyBehavior != HotkeyBehaviorHoldToTalk {
		t.Fatalf("default VoiceAgentHotkeyBehavior = %q, want %q", cfg.General.VoiceAgentHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	}
	if cfg.VoiceAgent.CloseBehavior != VoiceAgentCloseBehaviorContinue {
		t.Fatalf("default VoiceAgent.CloseBehavior = %q, want %q", cfg.VoiceAgent.CloseBehavior, VoiceAgentCloseBehaviorContinue)
	}
}

func TestNormalizeDictationProcessingMode(t *testing.T) {
	tests := []struct {
		in       string
		fallback string
		want     string
	}{
		{in: "", want: DictationProcessingModeFinalFull},
		{in: "final_full", want: DictationProcessingModeFinalFull},
		{in: "SEGMENT_BATCH", want: DictationProcessingModeSegmentBatch},
		{in: " provider_stream ", want: DictationProcessingModeProviderStream},
		{in: "auto", want: DictationProcessingModeAuto},
		{in: "bad", fallback: DictationProcessingModeSegmentBatch, want: DictationProcessingModeSegmentBatch},
		{in: "bad", fallback: "bad", want: DictationProcessingModeFinalFull},
	}
	for _, tt := range tests {
		if got := NormalizeDictationProcessingMode(tt.in, tt.fallback); got != tt.want {
			t.Fatalf("NormalizeDictationProcessingMode(%q, %q) = %q, want %q", tt.in, tt.fallback, got, tt.want)
		}
	}
}

func TestNormalizeAudioInputSource(t *testing.T) {
	tests := []struct {
		in       string
		fallback string
		want     string
	}{
		{in: "", want: AudioInputSourceMicrophone},
		{in: "microphone", want: AudioInputSourceMicrophone},
		{in: "system", want: AudioInputSourceSystemLoopback},
		{in: "loopback", want: AudioInputSourceSystemLoopback},
		{in: "MIC+SYSTEM", want: AudioInputSourceMicAndSystem},
		{in: "bad", fallback: AudioInputSourceSystemLoopback, want: AudioInputSourceSystemLoopback},
		{in: "bad", fallback: "bad", want: AudioInputSourceMicrophone},
	}
	for _, tt := range tests {
		if got := NormalizeAudioInputSource(tt.in, tt.fallback); got != tt.want {
			t.Fatalf("NormalizeAudioInputSource(%q, %q) = %q, want %q", tt.in, tt.fallback, got, tt.want)
		}
	}
}

func TestDefaultOverlayPosition(t *testing.T) {
	cfg := defaults()
	if cfg.UI.OverlayPosition != "bottom" {
		t.Fatalf("default OverlayPosition = %q, want %q", cfg.UI.OverlayPosition, "bottom")
	}
	if cfg.UI.OverlayMovable {
		t.Fatal("default OverlayMovable = true, want false")
	}
	if cfg.UI.OverlayFreeX != 0 || cfg.UI.OverlayFreeY != 0 {
		t.Fatalf("default free overlay coordinates = (%d,%d), want (0,0)", cfg.UI.OverlayFreeX, cfg.UI.OverlayFreeY)
	}
}

func TestDefaultStoreAudioSettings(t *testing.T) {
	cfg := defaults()
	if cfg.General.Hotkey != "ctrl+win" {
		t.Fatalf("default Hotkey = %q, want %q", cfg.General.Hotkey, "ctrl+win")
	}
	if cfg.General.DictateHotkey != "ctrl+win" {
		t.Fatalf("default DictateHotkey = %q, want %q", cfg.General.DictateHotkey, "ctrl+win")
	}
	if cfg.General.AssistHotkey != "win+alt" {
		t.Fatalf("default AssistHotkey = %q, want %q", cfg.General.AssistHotkey, "win+alt")
	}
	if cfg.General.AgentHotkey != "win+alt" {
		t.Fatalf("default AgentHotkey = %q, want %q", cfg.General.AgentHotkey, "win+alt")
	}
	if !cfg.Store.SaveAudio {
		t.Fatal("default Store.SaveAudio = false, want true")
	}
	if cfg.Store.AudioRetentionDays != 7 {
		t.Fatalf("default Store.AudioRetentionDays = %d, want %d", cfg.Store.AudioRetentionDays, 7)
	}
}

func TestSaveRoundTripNewFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := defaults()
	cfg.General.HotkeyMode = HotkeyBehaviorToggle
	cfg.General.DictateHotkeyBehavior = HotkeyBehaviorToggle
	cfg.General.AssistHotkeyBehavior = HotkeyBehaviorHoldToTalk
	cfg.General.VoiceAgentHotkeyBehavior = HotkeyBehaviorToggle
	cfg.General.DictationProcessingMode = DictationProcessingModeSegmentBatch
	cfg.Audio.InputSource = AudioInputSourceSystemLoopback
	cfg.UI.OverlayPosition = "bottom"
	cfg.UI.OverlayMovable = true
	cfg.UI.OverlayFreeX = 864
	cfg.UI.OverlayFreeY = 512

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.General.HotkeyMode != HotkeyBehaviorToggle {
		t.Fatalf("HotkeyMode = %q, want %q", reloaded.General.HotkeyMode, HotkeyBehaviorToggle)
	}
	if reloaded.General.DictateHotkeyBehavior != HotkeyBehaviorToggle {
		t.Fatalf("DictateHotkeyBehavior = %q, want %q", reloaded.General.DictateHotkeyBehavior, HotkeyBehaviorToggle)
	}
	if reloaded.General.AssistHotkeyBehavior != HotkeyBehaviorHoldToTalk {
		t.Fatalf("AssistHotkeyBehavior = %q, want %q", reloaded.General.AssistHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	}
	if reloaded.General.VoiceAgentHotkeyBehavior != HotkeyBehaviorToggle {
		t.Fatalf("VoiceAgentHotkeyBehavior = %q, want %q", reloaded.General.VoiceAgentHotkeyBehavior, HotkeyBehaviorToggle)
	}
	if reloaded.General.DictationProcessingMode != DictationProcessingModeSegmentBatch {
		t.Fatalf("DictationProcessingMode = %q, want %q", reloaded.General.DictationProcessingMode, DictationProcessingModeSegmentBatch)
	}
	if reloaded.Audio.InputSource != AudioInputSourceSystemLoopback {
		t.Fatalf("Audio.InputSource = %q, want %q", reloaded.Audio.InputSource, AudioInputSourceSystemLoopback)
	}
	if reloaded.UI.OverlayPosition != "bottom" {
		t.Fatalf("OverlayPosition = %q, want %q", reloaded.UI.OverlayPosition, "bottom")
	}
	if !reloaded.UI.OverlayMovable {
		t.Fatal("OverlayMovable = false, want true")
	}
	if reloaded.UI.OverlayFreeX != 864 || reloaded.UI.OverlayFreeY != 512 {
		t.Fatalf("free overlay coordinates = (%d,%d), want (864,512)", reloaded.UI.OverlayFreeX, reloaded.UI.OverlayFreeY)
	}
}

func TestLoadPreservesUnsetNewFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Write a config file that does NOT contain hotkey_mode or overlay_position.
	content := `[general]
language = "en"
hotkey = "ctrl+shift"
auto_stop_silence_ms = 300

[ui]
overlay_enabled = true
visualizer = "pill"
design = "default"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Fields absent from file should retain defaults.
	if cfg.General.HotkeyMode != HotkeyBehaviorHoldToTalk {
		t.Fatalf("HotkeyMode = %q, want default %q", cfg.General.HotkeyMode, HotkeyBehaviorHoldToTalk)
	}
	if cfg.General.DictateHotkeyBehavior != HotkeyBehaviorHoldToTalk {
		t.Fatalf("DictateHotkeyBehavior = %q, want default %q", cfg.General.DictateHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	}
	if cfg.General.AssistHotkeyBehavior != HotkeyBehaviorHoldToTalk {
		t.Fatalf("AssistHotkeyBehavior = %q, want default %q", cfg.General.AssistHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	}
	if cfg.General.VoiceAgentHotkeyBehavior != HotkeyBehaviorHoldToTalk {
		t.Fatalf("VoiceAgentHotkeyBehavior = %q, want default %q", cfg.General.VoiceAgentHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	}
	if cfg.VoiceAgent.CloseBehavior != VoiceAgentCloseBehaviorContinue {
		t.Fatalf("VoiceAgent.CloseBehavior = %q, want default %q", cfg.VoiceAgent.CloseBehavior, VoiceAgentCloseBehaviorContinue)
	}
	if cfg.General.DictationProcessingMode != DictationProcessingModeFinalFull {
		t.Fatalf("DictationProcessingMode = %q, want default %q", cfg.General.DictationProcessingMode, DictationProcessingModeFinalFull)
	}
	if cfg.Audio.InputSource != AudioInputSourceMicrophone {
		t.Fatalf("Audio.InputSource = %q, want default %q", cfg.Audio.InputSource, AudioInputSourceMicrophone)
	}
	if cfg.UI.OverlayPosition != "bottom" {
		t.Fatalf("OverlayPosition = %q, want default %q", cfg.UI.OverlayPosition, "bottom")
	}
}

func TestLoadBackfillsLegacyHotkeyModeIntoPerModeBehaviors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `[general]
hotkey_mode = "toggle"
dictate_hotkey = "win+alt"
assist_hotkey = "ctrl+win"
voice_agent_hotkey = "ctrl+shift"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.General.DictateHotkeyBehavior != HotkeyBehaviorToggle {
		t.Fatalf("DictateHotkeyBehavior = %q, want %q", cfg.General.DictateHotkeyBehavior, HotkeyBehaviorToggle)
	}
	if cfg.General.AssistHotkeyBehavior != HotkeyBehaviorToggle {
		t.Fatalf("AssistHotkeyBehavior = %q, want %q", cfg.General.AssistHotkeyBehavior, HotkeyBehaviorToggle)
	}
	if cfg.General.VoiceAgentHotkeyBehavior != HotkeyBehaviorToggle {
		t.Fatalf("VoiceAgentHotkeyBehavior = %q, want %q", cfg.General.VoiceAgentHotkeyBehavior, HotkeyBehaviorToggle)
	}
}

func TestLoadMigratesOldBuiltInHotkeyDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `[general]
hotkey = "win+alt"
dictate_hotkey = "win+alt"
assist_hotkey = "ctrl+win"
voice_agent_hotkey = "ctrl+shift"
agent_hotkey = "ctrl+win"
agent_mode = "assist"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := cfg.General.Hotkey, "ctrl+win"; got != want {
		t.Fatalf("legacy hotkey alias = %q, want %q", got, want)
	}
	if got, want := cfg.General.DictateHotkey, "ctrl+win"; got != want {
		t.Fatalf("dictate hotkey = %q, want %q", got, want)
	}
	if got, want := cfg.General.AssistHotkey, "win+alt"; got != want {
		t.Fatalf("assist hotkey = %q, want %q", got, want)
	}
	if got, want := cfg.General.AgentHotkey, "win+alt"; got != want {
		t.Fatalf("agent hotkey alias = %q, want %q", got, want)
	}
}

func TestLoadPreservesCustomHotkeyPair(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `[general]
hotkey = "ctrl+shift+d"
dictate_hotkey = "ctrl+shift+d"
assist_hotkey = "ctrl+win+j"
voice_agent_hotkey = "win+alt+k"
agent_hotkey = "ctrl+win+j"
agent_mode = "assist"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := cfg.General.DictateHotkey, "ctrl+shift+d"; got != want {
		t.Fatalf("dictate hotkey = %q, want %q", got, want)
	}
	if got, want := cfg.General.AssistHotkey, "ctrl+win+j"; got != want {
		t.Fatalf("assist hotkey = %q, want %q", got, want)
	}
	if got, want := cfg.General.VoiceAgentHotkey, "win+alt+k"; got != want {
		t.Fatalf("voice agent hotkey = %q, want %q", got, want)
	}
}

func TestApplyManagedIntegrationDefaultsSkipsNonCloudOnly(t *testing.T) {
	useMemorySecretStoreForTest(t)
	restoreBuild := OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaults()
	cfg.HuggingFace.Enabled = false // Explicitly disabled
	cfg.Routing.Strategy = "dynamic"
	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")
	t.Setenv("HF_TOKEN", "test-token")

	changed := ApplyManagedIntegrationDefaults(cfg)

	if changed {
		t.Fatal("ApplyManagedIntegrationDefaults should return false for non-cloud-only strategy")
	}
	if cfg.HuggingFace.Enabled {
		t.Fatal("huggingface should remain disabled when strategy is not cloud-only")
	}
}

func TestLoadBackfillsGeneralAutoStartFromLegacyVoiceAgentSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[general]
dictate_hotkey = "win+alt"
assist_hotkey = "ctrl+win"
voice_agent_hotkey = "ctrl+shift"

[voice_agent]
auto_start_on_launch = true
`

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.General.AutoStartOnLaunch {
		t.Fatal("General.AutoStartOnLaunch = false, want true from legacy voice_agent section")
	}
	if !cfg.VoiceAgent.AutoStartOnLaunch {
		t.Fatal("VoiceAgent.AutoStartOnLaunch = false, want true after sync")
	}
	if !cfg.General.StartAtLogin {
		t.Fatal("General.StartAtLogin = false, want true from legacy startup preference")
	}
}

func TestLoadPrefersGeneralAutoStartOverLegacyVoiceAgentSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[general]
dictate_hotkey = "win+alt"
assist_hotkey = "ctrl+win"
voice_agent_hotkey = "ctrl+shift"
auto_start_on_launch = false

[voice_agent]
auto_start_on_launch = true
`

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.General.AutoStartOnLaunch {
		t.Fatal("General.AutoStartOnLaunch = true, want explicit general setting to win")
	}
	if cfg.VoiceAgent.AutoStartOnLaunch {
		t.Fatal("VoiceAgent.AutoStartOnLaunch = true, want sync from explicit general setting")
	}
	if cfg.General.StartAtLogin {
		t.Fatal("General.StartAtLogin = true, want explicit dashboard auto-open setting to keep login start disabled")
	}
}

func TestLoadPrefersExplicitStartAtLoginOverLegacyAutoStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[general]
dictate_hotkey = "win+alt"
assist_hotkey = "ctrl+win"
voice_agent_hotkey = "ctrl+shift"
auto_start_on_launch = true
start_at_login = false
`

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.General.AutoStartOnLaunch {
		t.Fatal("General.AutoStartOnLaunch = false, want true from explicit config")
	}
	if cfg.General.StartAtLogin {
		t.Fatal("General.StartAtLogin = true, want explicit start_at_login=false to win")
	}
}

func TestApplyLocalInstallDefaultsPreparesPendingLocalInstallForOnboardingDownloads(t *testing.T) {
	cfg := defaults()
	cfg.Local.Enabled = false
	cfg.Routing.Strategy = "cloud-only"
	cfg.HuggingFace.Enabled = true
	state := &InstallState{Mode: InstallModeLocal}

	changed := ApplyLocalInstallDefaults(cfg, state)

	if !changed {
		t.Fatal("expected local install defaults to change config")
	}
	if !cfg.Local.Enabled {
		t.Fatal("local provider should be enabled for local-first installs")
	}
	if cfg.Routing.Strategy != "local-only" {
		t.Fatalf("routing strategy = %q, want %q", cfg.Routing.Strategy, "local-only")
	}
	if cfg.HuggingFace.Enabled {
		t.Fatal("HuggingFace should be disabled on fresh local install while onboarding is pending")
	}
	if cfg.Local.Model != DefaultLocalSTTModel {
		t.Fatalf("local model = %q, want %q", cfg.Local.Model, DefaultLocalSTTModel)
	}
}

func TestApplyLocalInstallDefaultsSkipsCompletedSetup(t *testing.T) {
	cfg := defaults()
	state := &InstallState{Mode: InstallModeLocal, SetupDone: true}

	changed := ApplyLocalInstallDefaults(cfg, state)

	if changed {
		t.Fatal("expected completed setup to keep config unchanged")
	}
	if !cfg.Local.Enabled {
		t.Fatal("local provider should remain enabled after setup is complete")
	}
	if cfg.Routing.Strategy != "local-only" {
		t.Fatalf("routing strategy = %q, want %q", cfg.Routing.Strategy, "local-only")
	}
}

func TestApplyLocalInstallDefaultsSkipsCloudInstalls(t *testing.T) {
	cfg := defaults()
	state := &InstallState{Mode: InstallModeCloud}

	changed := ApplyLocalInstallDefaults(cfg, state)

	if changed {
		t.Fatal("expected cloud installs to keep config unchanged")
	}
	if !cfg.Local.Enabled {
		t.Fatal("local provider should remain enabled unless a cloud install explicitly changes it")
	}
	if cfg.Routing.Strategy != "local-only" {
		t.Fatalf("routing strategy = %q, want %q", cfg.Routing.Strategy, "local-only")
	}
}

// --- InstallMode tests ---

func TestLoadMalformedTOMLFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Write garbage TOML that will fail to parse.
	if err := os.WriteFile(path, []byte("{{{{not valid toml!!!!"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load should not error on malformed TOML, got: %v", err)
	}
	if cfg.General.Language != "de" {
		t.Errorf("expected default language %q, got %q", "de", cfg.General.Language)
	}
	if cfg.General.Hotkey != "ctrl+win" {
		t.Errorf("expected default hotkey %q, got %q", "ctrl+win", cfg.General.Hotkey)
	}
}

func TestLoadInstallState_NoFile(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	state, err := LoadInstallState()
	if err != nil {
		t.Fatalf("LoadInstallState: %v", err)
	}
	if state.Mode != InstallModeNotSet {
		t.Fatalf("Mode = %q, want empty", state.Mode)
	}
	if state.DeviceID != "" {
		t.Fatalf("DeviceID = %q, want empty", state.DeviceID)
	}
	if state.SetupDone {
		t.Fatal("SetupDone should be false")
	}
}

func TestSaveAndLoadInstallState(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	state := &InstallState{Mode: InstallModeCloud}
	if err := SaveInstallState(state); err != nil {
		t.Fatalf("SaveInstallState: %v", err)
	}

	loaded, err := LoadInstallState()
	if err != nil {
		t.Fatalf("LoadInstallState: %v", err)
	}
	if loaded.Mode != InstallModeCloud {
		t.Fatalf("Mode = %q, want %q", loaded.Mode, InstallModeCloud)
	}
	if loaded.DeviceID == "" {
		t.Fatal("DeviceID should be set after save")
	}
}

func TestIsFirstRun_True(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	if !IsFirstRun() {
		t.Fatal("IsFirstRun should return true for empty APPDATA dir")
	}
}

func TestIsFirstRun_False(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	if err := SaveInstallState(&InstallState{Mode: InstallModeLocal}); err != nil {
		t.Fatalf("SaveInstallState: %v", err)
	}

	if IsFirstRun() {
		t.Fatal("IsFirstRun should return false after SaveInstallState")
	}
}

func TestSaveInstallState_GeneratesDeviceID(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	state := &InstallState{Mode: InstallModeLocal}
	if state.DeviceID != "" {
		t.Fatal("precondition: DeviceID should start empty")
	}

	if err := SaveInstallState(state); err != nil {
		t.Fatalf("SaveInstallState: %v", err)
	}

	loaded, err := LoadInstallState()
	if err != nil {
		t.Fatalf("LoadInstallState: %v", err)
	}
	if loaded.DeviceID == "" {
		t.Fatal("DeviceID should be generated on save")
	}
	if len(loaded.DeviceID) < 32 {
		t.Fatalf("DeviceID too short: %q", loaded.DeviceID)
	}
}

func TestInstallModeConstants(t *testing.T) {
	if InstallModeLocal != "local" {
		t.Fatalf("InstallModeLocal = %q, want %q", InstallModeLocal, "local")
	}
	if InstallModeCloud != "cloud" {
		t.Fatalf("InstallModeCloud = %q, want %q", InstallModeCloud, "cloud")
	}
	if InstallModeNotSet != "" {
		t.Fatalf("InstallModeNotSet = %q, want empty", InstallModeNotSet)
	}
}

func TestManagedHuggingFaceAvailableInBuild_DefaultsDisabledWhenUnset(t *testing.T) {
	restoreBuild := OverrideManagedHuggingFaceBuildForTests("")
	defer restoreBuild()

	if ManagedHuggingFaceAvailableInBuild() {
		t.Fatal("ManagedHuggingFaceAvailableInBuild() = true, want false without explicit build ldflag")
	}
}

func TestManagedHuggingFaceAvailableInBuild_PublicModuleFallbackStaysDisabled(t *testing.T) {
	restoreBuild := OverrideManagedHuggingFaceBuildForTests("")
	defer restoreBuild()

	if ManagedHuggingFaceAvailableInBuild() {
		t.Fatal("ManagedHuggingFaceAvailableInBuild() = true, want false without explicit build ldflag")
	}
}

func TestPhase0Defaults(t *testing.T) {
	cfg := defaults()

	const wantManifestURL = "https://api.github.com/repos/kombifyio/SpeechKit/releases/latest"
	if cfg.Update.ManifestURL != wantManifestURL {
		t.Errorf("Update.ManifestURL: want %q, got %q", wantManifestURL, cfg.Update.ManifestURL)
	}
	if cfg.Update.CheckIntervalHours != 6 {
		t.Errorf("Update.CheckIntervalHours: want 6, got %d", cfg.Update.CheckIntervalHours)
	}
	if !cfg.Update.Enabled {
		t.Errorf("Update.Enabled: want true by default, got false")
	}
	if cfg.Logging.MaxFileSizeMB != 50 {
		t.Errorf("Logging.MaxFileSizeMB: want 50, got %d", cfg.Logging.MaxFileSizeMB)
	}
	if cfg.Logging.MaxFiles != 30 {
		t.Errorf("Logging.MaxFiles: want 30, got %d", cfg.Logging.MaxFiles)
	}
	if cfg.Logging.Level != "off" {
		t.Errorf("Logging.Level: want \"off\" by default (privacy-first opt-in), got %q", cfg.Logging.Level)
	}
	if cfg.Audit.Enabled {
		t.Errorf("Audit.Enabled: want false by default (compliance log is opt-in), got true")
	}
	if cfg.Audit.RetentionDays != 90 {
		t.Errorf("Audit.RetentionDays: want 90 (retention applies when audit is opted in), got %d", cfg.Audit.RetentionDays)
	}
	if !cfg.Telemetry.UpdateCheck {
		t.Errorf("Telemetry.UpdateCheck: want true by default, got false")
	}
}

func TestLoadDisablesTelemetryWhenUpdateDisabled(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(tmpFile, []byte("[update]\nenabled = false\n"), 0o600); err != nil {
		t.Fatalf("write tmp config: %v", err)
	}
	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Update.Enabled {
		t.Errorf("Update.Enabled: want false (from toml), got true")
	}
	if cfg.Telemetry.UpdateCheck {
		t.Errorf("Telemetry.UpdateCheck: want false (backfilled from disabled update), got true")
	}
}

func TestEnterprisePresetsLoad(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		wantProv string
		wantUpd  bool
		wantStrt string
	}{
		{
			name:     "onprem profile A",
			path:     "../../deploy/presets/enterprise-onprem.toml",
			wantProv: "local-cascaded",
			wantUpd:  false,
			wantStrt: "local-only",
		},
		{
			name:     "cloud-byok profile B",
			path:     "../../deploy/presets/enterprise-cloud-byok.toml",
			wantProv: "gemini",
			wantUpd:  true,
			wantStrt: "dynamic",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(tc.path)
			if err != nil {
				t.Fatalf("Load(%s): %v", tc.path, err)
			}
			if cfg.VoiceAgent.Provider != tc.wantProv {
				t.Errorf("VoiceAgent.Provider: want %q, got %q", tc.wantProv, cfg.VoiceAgent.Provider)
			}
			if cfg.Update.Enabled != tc.wantUpd {
				t.Errorf("Update.Enabled: want %v, got %v", tc.wantUpd, cfg.Update.Enabled)
			}
			if cfg.Routing.Strategy != tc.wantStrt {
				t.Errorf("Routing.Strategy: want %q, got %q", tc.wantStrt, cfg.Routing.Strategy)
			}
		})
	}
}
