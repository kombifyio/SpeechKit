package gemini

import (
	"encoding/json"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"google.golang.org/genai"
)

func TestBuildGeminiLiveConnectConfigUsesDefaultInstruction(t *testing.T) {
	cfg := live.LiveConfig{
		Model:  "gemini-2.5-flash-native-audio-preview-12-2025",
		Locale: "de-DE",
	}

	connectCfg := buildGeminiLiveConnectConfig(cfg)

	if connectCfg == nil {
		t.Fatal("expected connect config")
	}
	if len(connectCfg.ResponseModalities) != 1 || connectCfg.ResponseModalities[0] != genai.ModalityAudio {
		t.Fatalf("response modalities = %#v, want [AUDIO]", connectCfg.ResponseModalities)
	}
	if connectCfg.SystemInstruction == nil {
		t.Fatal("expected system instruction")
	}

	text := joinContentText(connectCfg.SystemInstruction)
	if !strings.Contains(text, "allgemein") {
		t.Fatalf("default instruction = %q, want general helper guidance", text)
	}
	if connectCfg.InputAudioTranscription == nil {
		t.Fatal("expected input audio transcription to be enabled by default")
	}
	if len(connectCfg.InputAudioTranscription.LanguageCodes) != 0 {
		t.Fatalf("input transcription language codes = %#v, want none for Gemini API", connectCfg.InputAudioTranscription.LanguageCodes)
	}
	if connectCfg.OutputAudioTranscription == nil {
		t.Fatal("expected output audio transcription to be enabled by default")
	}
	if len(connectCfg.OutputAudioTranscription.LanguageCodes) != 0 {
		t.Fatalf("output transcription language codes = %#v, want none for Gemini API", connectCfg.OutputAudioTranscription.LanguageCodes)
	}
	if connectCfg.SpeechConfig == nil {
		t.Fatal("expected speech config")
	}
	if connectCfg.SpeechConfig.LanguageCode != "" {
		t.Fatalf("speech language code = %q, want empty for native audio live models", connectCfg.SpeechConfig.LanguageCode)
	}
	if connectCfg.SessionResumption == nil {
		t.Fatal("expected session resumption to be enabled by default")
	}
	if connectCfg.SessionResumption.Handle != "" {
		t.Fatalf("session resumption handle = %q, want empty for a fresh session", connectCfg.SessionResumption.Handle)
	}
	if connectCfg.SessionResumption.Transparent {
		t.Fatal("expected Gemini API session resumption to avoid transparent mode")
	}
	if connectCfg.ContextWindowCompression == nil {
		t.Fatal("expected context window compression to be configured by default")
	}
	if connectCfg.ContextWindowCompression.TriggerTokens == nil || *connectCfg.ContextWindowCompression.TriggerTokens != 12000 {
		t.Fatal("expected default context compression to trigger early for long-running voice dialogs")
	}
	if connectCfg.ContextWindowCompression.SlidingWindow == nil ||
		connectCfg.ContextWindowCompression.SlidingWindow.TargetTokens == nil ||
		*connectCfg.ContextWindowCompression.SlidingWindow.TargetTokens != 6000 {
		t.Fatal("expected default context compression to keep a compact rolling voice context")
	}
	if connectCfg.RealtimeInputConfig == nil || connectCfg.RealtimeInputConfig.AutomaticActivityDetection == nil {
		t.Fatal("expected realtime input config with automatic activity detection")
	}
}

func TestBuildGeminiLiveConnectConfigUsesCustomInstructionAndPolicies(t *testing.T) {
	cfg := live.LiveConfig{
		Model:            "gemini-2.5-flash-native-audio-preview-12-2025",
		Locale:           "en",
		Voice:            "Aoede",
		FrameworkPrompt:  "You moderate a cooperative game session. Keep turns short and clearly summarize decisions.",
		RefinementPrompt: "Address the user as captain and keep answers upbeat.",
		VocabularyHint:   "Important names: Acme Voice, Harbor Labs",
		Tools: []live.ToolDefinition{
			{
				Name:        "save_summary",
				Description: "Persist the current discussion summary",
				Behavior:    live.ToolBehaviorNonBlocking,
			},
		},
		Policies: live.LivePolicies{
			EnableInputAudioTranscription:  true,
			EnableOutputAudioTranscription: true,
			EnableAffectiveDialog:          true,
			Thinking: live.ThinkingPolicy{
				Enabled:         true,
				IncludeThoughts: true,
				ThinkingBudget:  128,
				ThinkingLevel:   live.ThinkingLevelMedium,
			},
			ContextCompression: live.ContextCompressionPolicy{
				Enabled:       true,
				TriggerTokens: 24000,
				TargetTokens:  12000,
			},
			ActivityDetection: live.ActivityDetectionPolicy{
				Automatic:         true,
				StartSensitivity:  live.StartSensitivityHigh,
				EndSensitivity:    live.EndSensitivityLow,
				PrefixPaddingMs:   80,
				SilenceDurationMs: 640,
				ActivityHandling:  live.ActivityHandlingStartOfActivityInterrupts,
				TurnCoverage:      live.TurnCoverageTurnIncludesOnlyActivity,
			},
		},
	}

	connectCfg := buildGeminiLiveConnectConfig(cfg)

	if connectCfg == nil {
		t.Fatal("expected connect config")
	}

	text := joinContentText(connectCfg.SystemInstruction)
	if !strings.Contains(text, "cooperative game session") {
		t.Fatalf("instruction = %q, want custom host instruction", text)
	}
	if !strings.Contains(text, "Address the user as captain") {
		t.Fatalf("instruction = %q, want refinement prompt guidance", text)
	}
	if strings.Index(text, "cooperative game session") > strings.Index(text, "Address the user as captain") {
		t.Fatalf("instruction = %q, want framework prompt before refinement prompt", text)
	}
	if !strings.Contains(text, "higher-priority framework") {
		t.Fatalf("instruction = %q, want precedence guidance for the refinement prompt", text)
	}
	if !strings.Contains(text, "Acme Voice") {
		t.Fatalf("instruction = %q, want vocabulary hint merged into instruction", text)
	}
	if connectCfg.ThinkingConfig == nil {
		t.Fatal("expected thinking config")
	}
	if connectCfg.ContextWindowCompression == nil || connectCfg.ContextWindowCompression.TriggerTokens == nil || *connectCfg.ContextWindowCompression.TriggerTokens != 24000 {
		t.Fatal("expected context compression trigger tokens to propagate")
	}
	if connectCfg.RealtimeInputConfig == nil || connectCfg.RealtimeInputConfig.AutomaticActivityDetection == nil {
		t.Fatal("expected automatic activity detection config")
	}
	if connectCfg.RealtimeInputConfig.AutomaticActivityDetection.PrefixPaddingMs == nil || *connectCfg.RealtimeInputConfig.AutomaticActivityDetection.PrefixPaddingMs != 80 {
		t.Fatal("expected VAD prefix padding to propagate")
	}
	if connectCfg.EnableAffectiveDialog == nil || !*connectCfg.EnableAffectiveDialog {
		t.Fatal("expected affective dialog to be enabled")
	}
	if len(connectCfg.Tools) != 1 || len(connectCfg.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("tools = %#v, want one mapped function declaration", connectCfg.Tools)
	}
	if got := connectCfg.Tools[0].FunctionDeclarations[0].Behavior; got != genai.BehaviorNonBlocking {
		t.Fatalf("tool behavior = %q, want %q", got, genai.BehaviorNonBlocking)
	}
}

func TestBuildGeminiLiveConnectConfigMapsProviderNeutralOptions(t *testing.T) {
	t.Parallel()
	connectCfg := buildGeminiLiveConnectConfig(live.LiveConfig{
		FrameworkPrompt: "Base host prompt.",
		ProviderOptions: provideropts.Values{
			provideropts.OptionContextPrompt:   "Current tenant prefers short German answers.",
			provideropts.OptionReasoningEffort: "high",
		},
	})
	text := joinContentText(connectCfg.SystemInstruction)
	if !strings.Contains(text, "Current tenant prefers short German answers.") {
		t.Fatalf("system instruction = %q", text)
	}
	if connectCfg.ThinkingConfig == nil {
		t.Fatal("expected reasoning_effort to enable thinking config")
	}
	if got := connectCfg.ThinkingConfig.ThinkingLevel; got != genai.ThinkingLevelHigh {
		t.Fatalf("thinking level = %q, want %q", got, genai.ThinkingLevelHigh)
	}
}

func TestBuildGeminiLiveConnectConfigUsesFrameworkPrompt(t *testing.T) {
	cfg := live.LiveConfig{
		Model:           "gemini-2.5-flash-native-audio-preview-12-2025",
		Locale:          "en",
		FrameworkPrompt: "Host framework instruction",
	}

	connectCfg := buildGeminiLiveConnectConfig(cfg)
	text := joinContentText(connectCfg.SystemInstruction)

	if !strings.Contains(text, "Host framework instruction") {
		t.Fatalf("instruction = %q, want framework prompt to remain effective", text)
	}
}

func TestGeminiLiveHTTPOptionsUseV1AlphaWhenRequired(t *testing.T) {
	tests := []struct {
		name string
		cfg  live.LiveConfig
		want string
	}{
		{
			name: "default live session stays on beta",
			cfg: live.LiveConfig{
				Model: "gemini-2.5-flash-native-audio-preview-12-2025",
			},
			want: "v1beta",
		},
		{
			name: "affective dialog uses alpha",
			cfg: live.LiveConfig{
				Model: "gemini-2.5-flash-native-audio-preview-12-2025",
				Policies: live.LivePolicies{
					EnableAffectiveDialog: true,
				},
			},
			want: "v1alpha",
		},
		{
			name: "ephemeral token uses alpha",
			cfg: live.LiveConfig{
				Model:  "gemini-2.5-flash-native-audio-preview-12-2025",
				APIKey: "auth_tokens/demo-token",
			},
			want: "v1alpha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildGeminiLiveHTTPOptions(tt.cfg)
			if got.APIVersion != tt.want {
				t.Fatalf("api version = %q, want %q", got.APIVersion, tt.want)
			}
		})
	}
}

func TestBuildGeminiLiveSessionResumptionConfigUsesHandleWithoutTransparentMode(t *testing.T) {
	tests := []struct {
		name       string
		handle     string
		wantHandle string
	}{
		{
			name:       "fresh session",
			handle:     "",
			wantHandle: "",
		},
		{
			name:       "reconnect with resume handle",
			handle:     "resume-handle-123",
			wantHandle: "resume-handle-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := buildGeminiLiveSessionResumptionConfig(tt.handle)
			if cfg == nil {
				t.Fatal("expected session resumption config")
			}
			if cfg.Handle != tt.wantHandle {
				t.Fatalf("handle = %q, want %q", cfg.Handle, tt.wantHandle)
			}
			if cfg.Transparent {
				t.Fatal("expected transparent mode to stay disabled for Gemini API")
			}
		})
	}
}

func TestBuildGeminiLiveConnectConfigOmitsGeminiAPIUnsupportedFields(t *testing.T) {
	cfg := live.LiveConfig{
		Model:  "gemini-2.5-flash-native-audio-preview-12-2025",
		Locale: "de-DE",
	}

	connectCfg := buildGeminiLiveConnectConfig(cfg)
	body, err := json.Marshal(connectCfg)
	if err != nil {
		t.Fatalf("marshal connect config: %v", err)
	}

	jsonBody := string(body)
	if strings.Contains(jsonBody, "languageCodes") {
		t.Fatalf("connect config JSON = %s, want no languageCodes for Gemini API", jsonBody)
	}
	if strings.Contains(jsonBody, "transparent") {
		t.Fatalf("connect config JSON = %s, want no transparent session resumption for Gemini API", jsonBody)
	}
}

func TestValidateGeminiLiveConfigRejectsUnsupportedGemini31Features(t *testing.T) {
	tests := []struct {
		name string
		cfg  live.LiveConfig
		want string
	}{
		{
			name: "affective dialog on gemini 3.1 live",
			cfg: live.LiveConfig{
				Model: "gemini-3.1-flash-live-preview",
				Policies: live.LivePolicies{
					EnableAffectiveDialog: true,
				},
			},
			want: "affective dialog",
		},
		{
			name: "non blocking tool on gemini 3.1 live",
			cfg: live.LiveConfig{
				Model: "gemini-3.1-flash-live-preview",
				Tools: []live.ToolDefinition{
					{
						Name:     "save_summary",
						Behavior: live.ToolBehaviorNonBlocking,
					},
				},
			},
			want: "non-blocking tool behavior",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGeminiLiveConfig(tt.cfg)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func joinContentText(content *genai.Content) string {
	if content == nil {
		return ""
	}
	parts := make([]string, 0, len(content.Parts))
	for _, part := range content.Parts {
		if part != nil && part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n")
}
