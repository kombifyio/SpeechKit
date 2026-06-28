package live

import (
	"strings"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

type LiveCapabilityFlag string

const (
	LiveCapabilityRealtimeAudio       LiveCapabilityFlag = "realtime_audio"
	LiveCapabilityToolCalling         LiveCapabilityFlag = "tool_calling"
	LiveCapabilityNativeWords         LiveCapabilityFlag = "native_words"
	LiveCapabilityTranscript          LiveCapabilityFlag = "transcript"
	LiveCapabilityInterruptions       LiveCapabilityFlag = "interruptions"
	LiveCapabilitySessionResume       LiveCapabilityFlag = "session_resume"
	LiveCapabilityNativeContextPrompt LiveCapabilityFlag = "native_context_prompt"
	LiveCapabilityNativeKeyterms      LiveCapabilityFlag = "native_keyterms"
	LiveCapabilityLanguageHints       LiveCapabilityFlag = "language_hints"
	LiveCapabilitySpeakerStreaming    LiveCapabilityFlag = "speaker_streaming"
	LiveCapabilityPrivacyRedaction    LiveCapabilityFlag = "privacy_redaction"
	LiveCapabilityVoiceFocus          LiveCapabilityFlag = "voice_focus"
	LiveCapabilityMedicalDomain       LiveCapabilityFlag = "medical_domain"
	LiveCapabilityReasoningEffort     LiveCapabilityFlag = "reasoning_effort"
	LiveCapabilityTranslation         LiveCapabilityFlag = "translation"
	LiveCapabilityTranscriptionOnly   LiveCapabilityFlag = "transcription_only"
)

type LiveModelDescriptor struct {
	Provider    string                   `json:"provider"`
	ModelID     string                   `json:"modelId"`
	Name        string                   `json:"name"`
	Lifecycle   framework.ModelLifecycle `json:"lifecycle"`
	Default     bool                     `json:"default,omitempty"`
	Recommended bool                     `json:"recommended,omitempty"`
	SourceURL   string                   `json:"sourceUrl"`
}

type ProviderDescriptor struct {
	Provider         string                  `json:"provider"`
	DisplayName      string                  `json:"displayName"`
	ProfileID        string                  `json:"profileId"`
	Capabilities     []LiveCapabilityFlag    `json:"capabilities"`
	Models           []LiveModelDescriptor   `json:"models,omitempty"`
	SupportedLocales []string                `json:"supportedLocales,omitempty"`
	NativeOptions    []provideropts.OptionID `json:"nativeOptions,omitempty"`
	AuthRequirement  string                  `json:"authRequirement,omitempty"`
	Transport        string                  `json:"transport,omitempty"`
	EvidenceURL      string                  `json:"evidenceUrl,omitempty"`
}

// HasCapability reports whether this provider advertises a capability.
func (d ProviderDescriptor) HasCapability(flag LiveCapabilityFlag) bool {
	for _, candidate := range d.Capabilities {
		if candidate == flag {
			return true
		}
	}
	return false
}

// DefaultModel returns the provider's default live model. If no model is
// explicitly marked as default, it falls back to the first advertised model.
func (d ProviderDescriptor) DefaultModel() (LiveModelDescriptor, bool) {
	for _, model := range d.Models {
		if model.Default {
			return model, true
		}
	}
	if len(d.Models) > 0 {
		return d.Models[0], true
	}
	return LiveModelDescriptor{}, false
}

// DefaultProviderDescriptors returns the public live-provider catalog for
// embedders that want to switch providers by name/profile/model at runtime.
func DefaultProviderDescriptors() []ProviderDescriptor {
	return []ProviderDescriptor{
		{
			Provider:    "google",
			DisplayName: "Gemini Live",
			ProfileID:   "realtime.google.gemini-native-audio",
			Capabilities: []LiveCapabilityFlag{
				LiveCapabilityRealtimeAudio,
				LiveCapabilityToolCalling,
				LiveCapabilityNativeContextPrompt,
				LiveCapabilityReasoningEffort,
				LiveCapabilitySessionResume,
				LiveCapabilityTranscript,
				LiveCapabilityInterruptions,
			},
			Models:           liveModelsForProviderProfile("google", "realtime.google.gemini-native-audio"),
			SupportedLocales: []string{"*"},
			NativeOptions:    nativeLiveOptions("google"),
			AuthRequirement:  "api_key",
			Transport:        "websocket",
			EvidenceURL:      "https://ai.google.dev/gemini-api/docs/live-guide",
		},
		{
			Provider:    "google",
			DisplayName: "Gemini Live Translate",
			ProfileID:   "realtime.google.gemini-live-translate",
			Capabilities: []LiveCapabilityFlag{
				LiveCapabilityRealtimeAudio,
				LiveCapabilityTranslation,
				LiveCapabilityTranscript,
				LiveCapabilityInterruptions,
			},
			Models:           liveModelsForProviderProfile("google", "realtime.google.gemini-live-translate"),
			SupportedLocales: []string{"*"},
			NativeOptions: []provideropts.OptionID{
				provideropts.OptionTranslation,
				provideropts.OptionTurnDetection,
			},
			AuthRequirement: "api_key",
			Transport:       "websocket",
			EvidenceURL:     "https://ai.google.dev/gemini-api/docs/live-api/translation",
		},
		{
			Provider:    "deepgram",
			DisplayName: "Deepgram Voice Agent",
			ProfileID:   "realtime.deepgram.voice-agent",
			Capabilities: []LiveCapabilityFlag{
				LiveCapabilityRealtimeAudio,
				LiveCapabilityToolCalling,
				LiveCapabilityNativeWords,
				LiveCapabilityNativeContextPrompt,
				LiveCapabilityNativeKeyterms,
				LiveCapabilityLanguageHints,
				LiveCapabilitySpeakerStreaming,
				LiveCapabilityTranscript,
				LiveCapabilityInterruptions,
			},
			Models:           liveModelsForProvider("deepgram"),
			SupportedLocales: []string{"en", "de", "es", "fr", "hi", "it", "ja", "nl", "pt", "ru"},
			NativeOptions:    nativeLiveOptions("deepgram"),
			AuthRequirement:  "api_key",
			Transport:        "websocket",
			EvidenceURL:      "https://developers.deepgram.com/docs/voice-agent-settings",
		},
		{
			Provider:    "assemblyai",
			DisplayName: "AssemblyAI Voice Agent",
			ProfileID:   "realtime.assemblyai.voice-agent",
			Capabilities: []LiveCapabilityFlag{
				LiveCapabilityRealtimeAudio,
				LiveCapabilityToolCalling,
				LiveCapabilityNativeWords,
				LiveCapabilityNativeContextPrompt,
				LiveCapabilityNativeKeyterms,
				LiveCapabilitySessionResume,
				LiveCapabilitySpeakerStreaming,
				LiveCapabilityTranscript,
				LiveCapabilityInterruptions,
			},
			Models:           liveModelsForProvider("assemblyai"),
			SupportedLocales: []string{"en", "es", "de", "fr", "pt", "it", "tr", "nl", "sv", "no", "da", "fi", "hi", "vi", "ar", "he", "ja", "zh"},
			NativeOptions:    nativeLiveOptions("assemblyai"),
			AuthRequirement:  "api_key",
			Transport:        "websocket",
			EvidenceURL:      "https://www.assemblyai.com/docs/voice-agents/voice-agent-api",
		},
		{
			Provider:    "openai",
			DisplayName: "OpenAI Realtime",
			ProfileID:   "realtime.openai.gpt-realtime-2",
			Capabilities: []LiveCapabilityFlag{
				LiveCapabilityRealtimeAudio,
				LiveCapabilityToolCalling,
				LiveCapabilityNativeContextPrompt,
				LiveCapabilityReasoningEffort,
				LiveCapabilityTranscript,
				LiveCapabilityInterruptions,
			},
			Models:           liveModelsForProvider("openai"),
			SupportedLocales: []string{"*"},
			NativeOptions:    nativeLiveOptions("openai"),
			AuthRequirement:  "api_key",
			Transport:        "websocket",
			EvidenceURL:      "https://platform.openai.com/docs/guides/realtime",
		},
	}
}

// FindProviderDescriptor resolves provider ids, provider aliases, and profile
// ids to the canonical public descriptor.
func FindProviderDescriptor(providerOrProfile string) (ProviderDescriptor, bool) {
	profileID := strings.TrimSpace(providerOrProfile)
	for _, descriptor := range DefaultProviderDescriptors() {
		if strings.EqualFold(descriptor.ProfileID, profileID) {
			return descriptor, true
		}
	}
	canonical := NormalizeProviderID(providerOrProfile)
	for _, descriptor := range DefaultProviderDescriptors() {
		if descriptor.Provider == canonical {
			return descriptor, true
		}
	}
	return ProviderDescriptor{}, false
}

// DefaultLiveConfigForProvider returns a provider/profile/model tuple suitable
// for constructing an embedded Voice Agent session. Callers still supply API
// keys, prompts, tools, and policies.
func DefaultLiveConfigForProvider(providerOrProfile string) (LiveConfig, bool) {
	descriptor, ok := FindProviderDescriptor(providerOrProfile)
	if !ok {
		return LiveConfig{}, false
	}
	cfg := LiveConfig{
		Provider:  descriptor.Provider,
		ProfileID: descriptor.ProfileID,
	}
	if model, ok := descriptor.DefaultModel(); ok {
		cfg.Model = model.ModelID
	}
	return cfg, true
}

// NormalizeProviderID maps common provider aliases and public profile ids to
// the canonical provider id used by [ProviderDescriptor.Provider].
func NormalizeProviderID(providerOrProfile string) string {
	value := strings.ToLower(strings.TrimSpace(providerOrProfile))
	value = strings.ReplaceAll(value, "_", "-")
	switch value {
	case "google", "gemini", "gemini-live", "google-live", "realtime.google.gemini-native-audio", "realtime.google.gemini-live-translate":
		return "google"
	case "deepgram", "deepgram-agent", "deepgram-live", "realtime.deepgram.voice-agent":
		return "deepgram"
	case "assemblyai", "assembly-ai", "assemblyai-agent", "assemblyai-live", "realtime.assemblyai.voice-agent":
		return "assemblyai"
	case "openai", "openai-realtime", "openai-live", "realtime.openai.gpt-realtime-2":
		return "openai"
	case "cascaded", "local-cascaded", "pipeline", "pipeline-fallback", "voice-agent-cascaded", "voice_agent.cascaded.pipeline":
		return "cascaded"
	default:
		return value
	}
}

func liveModelsForProvider(provider string) []LiveModelDescriptor {
	return liveModelsForProviderProfile(provider, "")
}

func liveModelsForProviderProfile(provider, profileID string) []LiveModelDescriptor {
	var out []LiveModelDescriptor
	for _, row := range framework.DefaultModelRegistry() {
		if row.Provider != provider || row.Mode != framework.ModeVoiceAgent {
			continue
		}
		if profileID != "" && !strings.EqualFold(row.ProfileID, profileID) {
			continue
		}
		out = append(out, LiveModelDescriptor{
			Provider:    row.Provider,
			ModelID:     row.ModelID,
			Name:        row.Name,
			Lifecycle:   row.Lifecycle,
			Default:     row.Default,
			Recommended: row.Recommended,
			SourceURL:   row.SourceURL,
		})
	}
	return out
}

func nativeLiveOptions(provider string) []provideropts.OptionID {
	manifest, ok := provideropts.FindManifest(provider, provideropts.ModalityVoiceAgent)
	if !ok {
		return nil
	}
	var out []provideropts.OptionID
	for _, opt := range manifest.Options {
		if opt.Status == provideropts.SupportNative {
			out = append(out, opt.ID)
		}
	}
	return out
}

func sessionCapabilitiesForProvider(provider string) SessionCapabilities {
	descriptor, ok := FindProviderDescriptor(provider)
	if !ok {
		return SessionCapabilities{Provider: NormalizeProviderID(provider)}
	}
	cfg, _ := DefaultLiveConfigForProvider(descriptor.Provider)
	return SessionCapabilities{
		Provider:     descriptor.Provider,
		ProfileID:    descriptor.ProfileID,
		Model:        cfg.Model,
		Capabilities: append([]LiveCapabilityFlag(nil), descriptor.Capabilities...),
	}
}
