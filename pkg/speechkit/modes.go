package speechkit

import (
	"fmt"
	"strings"
)

// Mode identifies one of SpeechKit's strict product modes.
type Mode string

const (
	ModeNone       Mode = "none"
	ModeDictation  Mode = "dictation"
	ModeAssist     Mode = "assist"
	ModeVoiceAgent Mode = "voice_agent"
)

// IntelligenceKind names the mode-specific intelligence contract.
type IntelligenceKind string

const (
	IntelligenceUser          IntelligenceKind = "user"
	IntelligenceUtility       IntelligenceKind = "utility"
	IntelligenceBrainstorming IntelligenceKind = "brainstorming"
)

// ProviderKind is the product-facing provider group shown for every mode.
type ProviderKind string

const (
	ProviderKindLocalBuiltIn   ProviderKind = "local_built_in"
	ProviderKindLocalProvider  ProviderKind = "local_provider"
	ProviderKindCloudProvider  ProviderKind = "cloud_provider"
	ProviderKindDirectProvider ProviderKind = "direct_provider"
)

// ExecutionMode describes the technical runtime behind a provider profile.
type ExecutionMode string

const (
	ExecutionModeLocal          ExecutionMode = "local"
	ExecutionModeSelfHostedHTTP ExecutionMode = "self_hosted_http"
	ExecutionModeHFRouted       ExecutionMode = "hf_routed"
	ExecutionModeOpenAI         ExecutionMode = "openai_api"
	ExecutionModeGroq           ExecutionMode = "groq_api"
	ExecutionModeGoogle         ExecutionMode = "google_api"
	ExecutionModeOllama         ExecutionMode = "ollama_local"
	ExecutionModeOpenRouter     ExecutionMode = "openrouter_api"
)

// Capability is a mode capability declared by a provider profile.
type Capability string

const (
	CapabilityTranscription         Capability = "transcription"
	CapabilitySTT                   Capability = "stt"
	CapabilityAudioInput            Capability = "audio_input"
	CapabilityLLM                   Capability = "llm"
	CapabilityTTS                   Capability = "tts"
	CapabilityRealtimeAudio         Capability = "realtime_audio"
	CapabilityPipelineFallback      Capability = "pipeline_fallback"
	CapabilityToolCalling           Capability = "tool_calling"
	CapabilityDictionaryPrompt      Capability = "dictionary_prompt"
	CapabilityDictionaryNativeHints Capability = "dictionary_native_hints"
	CapabilitySessionSummary        Capability = "session_summary"
)

// ModelVariant is a concrete model choice inside a provider profile group.
type ModelVariant struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ModelID     string `json:"modelId"`
	Description string `json:"description,omitempty"`
	Recommended bool   `json:"recommended,omitempty"`
}

// ProviderProfile is the public catalog entry host applications can present or
// activate. ProviderKind is the stable user-facing grouping; ExecutionMode is
// the technical adapter underneath it.
type ProviderProfile struct {
	ID             string         `json:"id"`
	Mode           Mode           `json:"mode"`
	Name           string         `json:"name"`
	ProviderKind   ProviderKind   `json:"providerKind"`
	ExecutionMode  ExecutionMode  `json:"executionMode,omitempty"`
	ModelID        string         `json:"modelId,omitempty"`
	Source         string         `json:"source,omitempty"`
	Description    string         `json:"description,omitempty"`
	License        string         `json:"license,omitempty"`
	Capabilities   []Capability   `json:"capabilities,omitempty"`
	AdapterKind    string         `json:"adapterKind,omitempty"`
	Variants       []ModelVariant `json:"variants,omitempty"`
	AllowInference bool           `json:"inferenceAllowed,omitempty"`
	Default        bool           `json:"default,omitempty"`
	Recommended    bool           `json:"recommended,omitempty"`
	Experimental   bool           `json:"experimental,omitempty"`
}

func (p ProviderProfile) HasCapability(capability Capability) bool {
	for _, candidate := range p.Capabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}

// ModeContract documents what a mode may and may not do. Hosts can use this to
// validate custom adapters before exposing them to users.
type ModeContract struct {
	Mode         Mode             `json:"mode"`
	Intelligence IntelligenceKind `json:"intelligence"`
	Input        string           `json:"input"`
	Output       string           `json:"output"`
	Allowed      []Capability     `json:"allowed"`
	Forbidden    []Capability     `json:"forbidden"`
}

// ModeSetting is the public per-mode configuration shape used by the SDK and
// the versioned HTTP control plane.
type ModeSetting struct {
	Enabled           bool   `json:"enabled"`
	Hotkey            string `json:"hotkey,omitempty"`
	HotkeyBehavior    string `json:"hotkeyBehavior,omitempty"`
	PrimaryProfileID  string `json:"primaryProfileId,omitempty"`
	FallbackProfileID string `json:"fallbackProfileId,omitempty"`
}

type DictationSetting struct {
	ModeSetting
	DictionaryEnabled bool `json:"dictionaryEnabled"`
}

type AssistSetting struct {
	ModeSetting
	TTSEnabled      bool   `json:"ttsEnabled"`
	UtilityRegistry string `json:"utilityRegistry,omitempty"`
}

type VoiceAgentSetting struct {
	ModeSetting
	SessionSummary   bool   `json:"sessionSummary"`
	PipelineFallback bool   `json:"pipelineFallback"`
	CloseBehavior    string `json:"closeBehavior,omitempty"`
}

type ModeSettings struct {
	Dictation  DictationSetting  `json:"dictation"`
	Assist     AssistSetting     `json:"assist"`
	VoiceAgent VoiceAgentSetting `json:"voiceAgent"`
}

const ReadinessSchemaVersion = "provider-readiness.v1"

// ReadinessRequirement is a machine-readable setup check for a provider
// profile. Hosts can render these checks directly instead of hard-coding
// provider-specific setup rules.
type ReadinessRequirement struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Category string `json:"category"`
	Required bool   `json:"required"`
	Ready    bool   `json:"ready"`
	Missing  string `json:"missing,omitempty"`
}

// ReadinessAction describes the next setup command a host can expose when a
// requirement is not ready.
type ReadinessAction struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
}

// ReadinessArtifact describes downloadable or pullable model artifacts tied to
// a provider profile. Local Built-in profiles use this to expose concrete model
// choices through the same readiness API as credentials and runtime checks.
type ReadinessArtifact struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	SizeLabel      string `json:"sizeLabel,omitempty"`
	SizeBytes      int64  `json:"sizeBytes,omitempty"`
	Available      bool   `json:"available"`
	Selected       bool   `json:"selected"`
	RuntimeReady   bool   `json:"runtimeReady,omitempty"`
	RuntimeProblem string `json:"runtimeProblem,omitempty"`
	Recommended    bool   `json:"recommended,omitempty"`
}

// Readiness describes whether a provider profile can be used right now.
type Readiness struct {
	SchemaVersion    string                 `json:"schemaVersion,omitempty"`
	ProfileID        string                 `json:"profileId"`
	Mode             Mode                   `json:"mode"`
	ProviderKind     ProviderKind           `json:"providerKind"`
	ExecutionMode    ExecutionMode          `json:"executionMode,omitempty"`
	ModelID          string                 `json:"modelId,omitempty"`
	Source           string                 `json:"source,omitempty"`
	Active           bool                   `json:"active"`
	Default          bool                   `json:"default"`
	Configured       bool                   `json:"configured"`
	CredentialsReady bool                   `json:"credentialsReady"`
	RuntimeReady     bool                   `json:"runtimeReady"`
	CapabilityReady  bool                   `json:"capabilityReady"`
	Ready            bool                   `json:"ready"`
	Missing          []string               `json:"missing,omitempty"`
	Requirements     []ReadinessRequirement `json:"requirements,omitempty"`
	Actions          []ReadinessAction      `json:"actions,omitempty"`
	Artifacts        []ReadinessArtifact    `json:"artifacts,omitempty"`
}

// RequiredCapabilities returns the minimum capability set for a profile to
// satisfy a mode contract.
func RequiredCapabilities(mode Mode, nativeRealtime bool) []Capability {
	switch NormalizeMode(mode) {
	case ModeDictation:
		return []Capability{CapabilityTranscription}
	case ModeAssist:
		return []Capability{CapabilityLLM}
	case ModeVoiceAgent:
		if nativeRealtime {
			return []Capability{CapabilityRealtimeAudio}
		}
		return []Capability{CapabilityPipelineFallback, CapabilitySessionSummary}
	default:
		return nil
	}
}

func DefaultModeContracts() []ModeContract {
	return []ModeContract{
		{
			Mode:         ModeDictation,
			Intelligence: IntelligenceUser,
			Input:        "audio",
			Output:       "text",
			Allowed:      []Capability{CapabilityTranscription, CapabilitySTT, CapabilityAudioInput, CapabilityDictionaryPrompt, CapabilityDictionaryNativeHints},
			Forbidden:    []Capability{CapabilityToolCalling, CapabilityLLM, CapabilityRealtimeAudio, CapabilityTTS},
		},
		{
			Mode:         ModeAssist,
			Intelligence: IntelligenceUtility,
			Input:        "audio_or_text_with_optional_context",
			Output:       "one_shot_result",
			Allowed:      []Capability{CapabilityLLM, CapabilityToolCalling, CapabilityTTS, CapabilitySessionSummary},
			Forbidden:    []Capability{CapabilityRealtimeAudio},
		},
		{
			Mode:         ModeVoiceAgent,
			Intelligence: IntelligenceBrainstorming,
			Input:        "realtime_audio_dialogue",
			Output:       "dialogue_transcript_and_optional_summary",
			Allowed:      []Capability{CapabilityRealtimeAudio, CapabilityPipelineFallback, CapabilityAudioInput, CapabilityTTS, CapabilitySessionSummary, CapabilityToolCalling},
			Forbidden:    []Capability{CapabilityTranscription},
		},
	}
}

func NormalizeMode(mode Mode) Mode {
	switch strings.TrimSpace(string(mode)) {
	case "dictate", "dictation", "transcribe", "stt":
		return ModeDictation
	case "assist":
		return ModeAssist
	case "voice_agent", "voiceAgent", "voice-agent", "realtime_voice":
		return ModeVoiceAgent
	case "none", "":
		return ModeNone
	default:
		return ModeNone
	}
}

// ValidateProfileForMode checks the stable v23 mode capability contract.
func ValidateProfileForMode(profile ProviderProfile, mode Mode) error {
	mode = NormalizeMode(mode)
	if mode == ModeNone {
		return fmt.Errorf("unsupported mode %q", profile.Mode)
	}
	if NormalizeMode(profile.Mode) != mode {
		return fmt.Errorf("profile %q belongs to mode %q, not %q", profile.ID, profile.Mode, mode)
	}

	nativeRealtime := mode == ModeVoiceAgent && profile.HasCapability(CapabilityRealtimeAudio)
	for _, required := range RequiredCapabilities(mode, nativeRealtime) {
		if !profile.HasCapability(required) {
			return fmt.Errorf("profile %q missing required capability %q for %q", profile.ID, required, mode)
		}
	}
	if mode == ModeDictation && (profile.HasCapability(CapabilityToolCalling) || profile.HasCapability(CapabilityLLM)) {
		return fmt.Errorf("dictation profile %q cannot expose tools or LLM rewriting", profile.ID)
	}
	return nil
}
