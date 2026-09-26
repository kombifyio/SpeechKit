package speechkit

import (
	"fmt"
	"strings"
)

// Mode identifies one of SpeechKit's strict product modes.
type Mode string

// Mode values. ModeNone means no mode is selected; ModeDictation, ModeAssist
// and ModeVoiceAgent are the three interaction modes; ModeTTS is a
// model-selection axis rather than an interaction mode. [NormalizeMode] maps
// aliases such as "dictate" or "voice-agent" onto these values.
const (
	ModeNone       Mode = "none"
	ModeDictation  Mode = "dictation"
	ModeAssist     Mode = "assist"
	ModeVoiceAgent Mode = "voice_agent"
	// ModeTTS exposes Text-to-Speech as a first-class model-selection axis
	// alongside the three product modes. The host activates an Assist or
	// Voice-Agent session and the TTS profile selected here drives which
	// provider speaks the response. v0.37 introduced this alongside the
	// Voice-Companion hands-free flow — Thalia + Companion Live need a
	// stable place to pin a TTS voice across deployments.
	ModeTTS Mode = "tts"
)

// IntelligenceKind names the mode-specific intelligence contract.
type IntelligenceKind string

// Intelligence contracts, one per mode as assigned by [DefaultModeContracts]:
// Dictation runs on User intelligence (the user's own words, unchanged),
// Assist on Utility intelligence (one-shot utilities and tools), Voice Agent
// on Brainstorming intelligence (open realtime dialogue) and TTS on Voice
// Output.
const (
	IntelligenceUser          IntelligenceKind = "user"
	IntelligenceUtility       IntelligenceKind = "utility"
	IntelligenceBrainstorming IntelligenceKind = "brainstorming"
	// IntelligenceVoiceOutput is the contract for the TTS mode: render
	// generated text to audio. No user intelligence, no utility tools,
	// no brainstorming — strictly text in, audio out.
	IntelligenceVoiceOutput IntelligenceKind = "voice_output"
)

// ProviderKind is the product-facing provider group shown for every mode.
type ProviderKind string

// Provider kinds, the user-facing grouping of a profile: Local Built-in is the
// SpeechKit-managed local runtime and model artifact path; Local Provider is a
// user-managed local runtime such as Ollama or another local OpenAI-compatible
// service; Cloud Provider is a routed cloud or hosted open-weight provider;
// Direct Provider is a direct model-vendor API. [NetworkScope] admits profiles
// by kind.
const (
	ProviderKindLocalBuiltIn   ProviderKind = "local_built_in"
	ProviderKindLocalProvider  ProviderKind = "local_provider"
	ProviderKindCloudProvider  ProviderKind = "cloud_provider"
	ProviderKindDirectProvider ProviderKind = "direct_provider"
)

// ExecutionMode describes the technical runtime behind a provider profile.
type ExecutionMode string

// Execution modes, the technical adapter behind a profile: the in-process
// local runtime, a self-hosted OpenAI-compatible HTTP service, Hugging Face
// routed inference, the vendor APIs of OpenAI, Groq, Google (opt-in BYOK),
// Deepgram, AssemblyAI, OpenRouter and Foundry, and a local Ollama server.
const (
	ExecutionModeLocal          ExecutionMode = "local"
	ExecutionModeSelfHostedHTTP ExecutionMode = "self_hosted_http"
	ExecutionModeHFRouted       ExecutionMode = "hf_routed"
	ExecutionModeOpenAI         ExecutionMode = "openai_api"
	ExecutionModeGroq           ExecutionMode = "groq_api"
	ExecutionModeGoogle         ExecutionMode = "google_api"
	ExecutionModeDeepgram       ExecutionMode = "deepgram_api"
	ExecutionModeAssemblyAI     ExecutionMode = "assemblyai_api"
	ExecutionModeOllama         ExecutionMode = "ollama_local"
	ExecutionModeOpenRouter     ExecutionMode = "openrouter_api"
	ExecutionModeFoundry        ExecutionMode = "foundry_api"
)

// Capability is a mode capability declared by a provider profile.
type Capability string

// Capabilities a provider profile can declare. [DefaultModeContracts] lists
// the allowed and forbidden set per mode and [RequiredCapabilities] the
// minimum a profile needs. Every STT provider reports CapabilityTranscription,
// CapabilitySTT and CapabilityAudioInput.
const (
	CapabilityTranscription Capability = "transcription"
	CapabilitySTT           Capability = "stt"
	CapabilityAudioInput    Capability = "audio_input"
	CapabilityLLM           Capability = "llm"
	CapabilityTTS           Capability = "tts"
	// CapabilityRealtimeAudio is native audio-to-audio dialogue;
	// CapabilityPipelineFallback is the STT -> LLM -> TTS chain used by Voice
	// Agent profiles without it.
	CapabilityRealtimeAudio    Capability = "realtime_audio"
	CapabilityPipelineFallback Capability = "pipeline_fallback"
	CapabilityToolCalling      Capability = "tool_calling"
	// The *Prompt variants pass the user dictionary or Words as prompt text;
	// the *NativeHints variants use the provider's own vocabulary-boosting
	// parameters instead.
	CapabilityDictionaryPrompt      Capability = "dictionary_prompt"
	CapabilityDictionaryNativeHints Capability = "dictionary_native_hints"
	CapabilityWordsPrompt           Capability = "words_prompt"
	CapabilityWordsNativeHints      Capability = "words_native_hints"
	// CapabilityPostSTTReplacements applies text replacements after
	// recognition; CapabilitySessionSummary produces a structured summary
	// when a Voice Agent session ends.
	CapabilityPostSTTReplacements Capability = "post_stt_replacements"
	CapabilitySessionSummary      Capability = "session_summary"
	// Realtime dialogue features: a live transcript of the dialogue, user
	// barge-in, and resuming a session.
	CapabilityTranscript    Capability = "transcript"
	CapabilityInterruptions Capability = "interruptions"
	CapabilitySessionResume Capability = "session_resume"
	// CapabilityNativeContextPrompt accepts caller-supplied situational
	// context and CapabilityNativeKeyterms provider-side key terms;
	// CapabilityNativeDictationStream marks a [DictationStreamProvider];
	// CapabilityLanguageHints biases recognition toward given BCP-47 languages.
	CapabilityNativeContextPrompt   Capability = "native_context_prompt"
	CapabilityNativeKeyterms        Capability = "native_keyterms"
	CapabilityNativeDictationStream Capability = "native_dictation_stream"
	CapabilityLanguageHints         Capability = "language_hints"
	// Provider-native options: speaker labels on a live stream, PII
	// redaction, voice focus (suppressing background voices), a medical
	// vocabulary domain, configurable reasoning effort, translation, and a
	// realtime session that only transcribes.
	CapabilitySpeakerStreaming  Capability = "speaker_streaming"
	CapabilityPrivacyRedaction  Capability = "privacy_redaction"
	CapabilityVoiceFocus        Capability = "voice_focus"
	CapabilityMedicalDomain     Capability = "medical_domain"
	CapabilityReasoningEffort   Capability = "reasoning_effort"
	CapabilityTranslation       Capability = "translation"
	CapabilityTranscriptionOnly Capability = "transcription_only"
	// Speaker layers (see the speaker package): diarization tells speakers
	// apart, identification matches a known person, attribution names turns,
	// and enrollment registers a voice.
	CapabilitySpeakerDiarization    Capability = "speaker_diarization"
	CapabilitySpeakerIdentification Capability = "speaker_identification"
	CapabilitySpeakerAttribution    Capability = "speaker_attribution"
	CapabilitySpeakerEnrollment     Capability = "speaker_enrollment"
)

// Modality classifies what a catalog entry does, independent of the three
// user-facing modes. Every profile a user can select maps onto a Mode as
// well; support entries a host needs but a user never picks — embeddings,
// rerankers, utility models — only have a Modality.
type Modality string

// Modalities. STT, TTS, realtime voice and assist map onto a user-facing mode
// (see [ModeForModality]); utility, embedding and reranker entries are
// support models a host needs but a user never picks.
const (
	ModalitySTT           Modality = "stt"
	ModalityTTS           Modality = "tts"
	ModalityRealtimeVoice Modality = "realtime_voice"
	ModalityAssist        Modality = "assist"
	ModalityUtility       Modality = "utility"
	ModalityEmbedding     Modality = "embedding"
	ModalityReranker      Modality = "reranker"
)

// ModalityForMode returns the modality a user-facing mode runs as, or "" for
// a mode that has none.
func ModalityForMode(mode Mode) Modality {
	switch NormalizeMode(mode) {
	case ModeDictation:
		return ModalitySTT
	case ModeAssist:
		return ModalityAssist
	case ModeVoiceAgent:
		return ModalityRealtimeVoice
	case ModeTTS:
		return ModalityTTS
	default:
		return ""
	}
}

// ModeForModality returns the user-facing mode a modality is selectable in,
// or ModeNone for support modalities a user never picks directly.
func ModeForModality(modality Modality) Mode {
	switch modality {
	case ModalitySTT:
		return ModeDictation
	case ModalityAssist:
		return ModeAssist
	case ModalityRealtimeVoice:
		return ModeVoiceAgent
	case ModalityTTS:
		return ModeTTS
	default:
		return ModeNone
	}
}

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
	ID   string `json:"id"`
	Mode Mode   `json:"mode"`
	// Modality is what the entry does. ProviderProfileWithDefaults derives it
	// from Mode when unset, and derives Mode from it for support entries that
	// carry no mode.
	Modality         Modality       `json:"modality,omitempty"`
	Name             string         `json:"name"`
	ProviderKind     ProviderKind   `json:"providerKind"`
	ExecutionMode    ExecutionMode  `json:"executionMode,omitempty"`
	Provider         string         `json:"provider,omitempty"`
	ModelID          string         `json:"modelId,omitempty"`
	Lifecycle        ModelLifecycle `json:"lifecycle,omitempty"`
	Source           string         `json:"source,omitempty"`
	Description      string         `json:"description,omitempty"`
	License          string         `json:"license,omitempty"`
	Capabilities     []Capability   `json:"capabilities,omitempty"`
	SupportedLocales []string       `json:"supportedLocales,omitempty"`
	NativeOptions    []string       `json:"nativeOptions,omitempty"`
	AuthRequirement  string         `json:"authRequirement,omitempty"`
	Transport        string         `json:"transport,omitempty"`
	EvidenceURL      string         `json:"evidenceUrl,omitempty"`
	AdapterKind      string         `json:"adapterKind,omitempty"`
	Variants         []ModelVariant `json:"variants,omitempty"`
	AllowInference   bool           `json:"inferenceAllowed,omitempty"`
	Default          bool           `json:"default,omitempty"`
	Recommended      bool           `json:"recommended,omitempty"`
	Experimental     bool           `json:"experimental,omitempty"`
}

// NormalizeProviderProfileID maps legacy profile IDs to their current
// canonical IDs while preserving unknown custom IDs.
func NormalizeProviderProfileID(profileID string) string {
	profileID = strings.TrimSpace(profileID)
	switch profileID {
	case "stt.google.chirp-3":
		return "stt.google.latest-long"
	case "stt.google.chirp-3-diarization":
		return "stt.google.latest-long-diarization"
	case "assist.foundry.gpt-5.1":
		// The Foundry assist profile moved to the GPT-5.6 family; configs
		// written before that keep resolving to the same profile.
		return "assist.foundry.gpt-5.6"
	case "stt.foundry.gpt-4o-mini-transcribe":
		// The GPT-4o transcription profile was retired in favour of the
		// Microsoft speech model the resource serves without a deployment.
		return "stt.foundry.mai-transcribe-2"
	case "tts.foundry.gpt-4o-mini-tts":
		return "tts.foundry.mai-voice-2"
	default:
		return profileID
	}
}

// HasCapability reports whether the profile declares capability.
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
	// ModeSource is "local" (default) or "server". When "server", this mode
	// runs against the speechkit-server pointed to by ServerConnection
	// instead of the in-process Framework kernel. Empty/missing is treated
	// as "local" for backwards compatibility with pre-0.26 hosts.
	ModeSource string `json:"modeSource,omitempty"`
}

// DictationSetting is the Dictation mode configuration: the shared
// [ModeSetting] plus whether the user dictionary is applied.
type DictationSetting struct {
	ModeSetting
	DictionaryEnabled bool `json:"dictionaryEnabled"`
}

// AssistSetting is the Assist mode configuration: the shared [ModeSetting],
// whether results are spoken through TTS, and the name of the utility
// registry in use.
type AssistSetting struct {
	ModeSetting
	TTSEnabled      bool   `json:"ttsEnabled"`
	UtilityRegistry string `json:"utilityRegistry,omitempty"`
}

// VoiceAgentSetting is the Voice Agent mode configuration: the shared
// [ModeSetting], whether sessions end with a summary, whether the STT/LLM/TTS
// pipeline fallback is allowed, the close behavior, and the selected agent
// profile and agent sequence.
type VoiceAgentSetting struct {
	ModeSetting
	SessionSummary   bool   `json:"sessionSummary"`
	PipelineFallback bool   `json:"pipelineFallback"`
	CloseBehavior    string `json:"closeBehavior,omitempty"`
	AgentProfileID   string `json:"agentProfileId,omitempty"`
	AgentSequenceID  string `json:"agentSequenceId,omitempty"`
}

// ModeSettings is the complete per-mode configuration exchanged between the
// SDK, hostconfig and the versioned HTTP control plane.
type ModeSettings struct {
	Dictation        DictationSetting        `json:"dictation"`
	Assist           AssistSetting           `json:"assist"`
	VoiceAgent       VoiceAgentSetting       `json:"voiceAgent"`
	ServerConnection ServerConnectionSetting `json:"serverConnection"`
}

// ServerConnectionSetting exposes the [server_connection] config section
// to the control-plane API + frontend. The bearer token is never sent
// across this boundary — only the env var name + connection metadata.
type ServerConnectionSetting struct {
	Enabled              bool                     `json:"enabled"`
	ActiveTargetID       string                   `json:"activeTargetId,omitempty"`
	URL                  string                   `json:"url"`
	BearerTokenEnv       string                   `json:"bearerTokenEnv,omitempty"`
	AuthMode             string                   `json:"authMode,omitempty"`
	BetaInstallIDEnv     string                   `json:"betaInstallIdEnv,omitempty"`
	BetaInstallSecretEnv string                   `json:"betaInstallSecretEnv,omitempty"`
	BearerTokenSet       bool                     `json:"bearerTokenSet"`
	FallbackToLocal      bool                     `json:"fallbackToLocal"`
	RequestTimeoutSec    int                      `json:"requestTimeoutSec"`
	Targets              []ServerConnectionTarget `json:"targets,omitempty"`
}

// ServerConnectionTarget is one registered speechkit-server the host can
// connect to; ServerConnectionSetting.ActiveTargetID selects the current one.
// Like its parent it carries only env var names for credentials, never values.
type ServerConnectionTarget struct {
	ID                   string `json:"id"`
	Label                string `json:"label"`
	URL                  string `json:"url"`
	AuthMode             string `json:"authMode"`
	BearerTokenEnv       string `json:"bearerTokenEnv,omitempty"`
	BetaInstallIDEnv     string `json:"betaInstallIdEnv,omitempty"`
	BetaInstallSecretEnv string `json:"betaInstallSecretEnv,omitempty"`
	BearerTokenSet       bool   `json:"bearerTokenSet"`
	FallbackToLocal      bool   `json:"fallbackToLocal"`
	RequestTimeoutSec    int    `json:"requestTimeoutSec"`
}

// ReadinessSchemaVersion is the schema identifier the readiness API sets in
// [Readiness.SchemaVersion].
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
	SchemaVersion    string        `json:"schemaVersion,omitempty"`
	ProfileID        string        `json:"profileId"`
	Mode             Mode          `json:"mode"`
	ProviderKind     ProviderKind  `json:"providerKind"`
	ExecutionMode    ExecutionMode `json:"executionMode,omitempty"`
	ModelID          string        `json:"modelId,omitempty"`
	Source           string        `json:"source,omitempty"`
	Active           bool          `json:"active"`
	Default          bool          `json:"default"`
	Configured       bool          `json:"configured"`
	CredentialsReady bool          `json:"credentialsReady"`
	RuntimeReady     bool          `json:"runtimeReady"`
	CapabilityReady  bool          `json:"capabilityReady"`
	Ready            bool          `json:"ready"`
	// BlockedByScope reports that the active NetworkScope forbids this
	// profile. When true, DisabledReasonID carries the stable localizable
	// reason ID and Ready is forced false. Absent/false on targets without
	// a network scope (e.g. the Server-Target catalog).
	BlockedByScope   bool                   `json:"blockedByScope,omitempty"`
	DisabledReasonID string                 `json:"disabledReasonId,omitempty"`
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
	case ModeTTS:
		return []Capability{CapabilityTTS}
	default:
		return nil
	}
}

// DefaultModeContracts returns the built-in [ModeContract] of Dictation,
// Assist, Voice Agent and TTS as a fresh slice callers may modify.
func DefaultModeContracts() []ModeContract {
	return []ModeContract{
		{
			Mode:         ModeDictation,
			Intelligence: IntelligenceUser,
			Input:        "audio",
			Output:       "text",
			// native_context_prompt joined the dictation contract with
			// AssemblyAI Universal-3.5 Pro: the sync endpoint accepts
			// conversation_context and the realtime session agent_context, so
			// a dictation profile may legitimately advertise that its speech
			// model conditions on caller-supplied situational context.
			Allowed:   []Capability{CapabilityTranscription, CapabilitySTT, CapabilityAudioInput, CapabilityDictionaryPrompt, CapabilityDictionaryNativeHints, CapabilityWordsPrompt, CapabilityWordsNativeHints, CapabilityPostSTTReplacements, CapabilityNativeDictationStream, CapabilityNativeContextPrompt, CapabilitySpeakerDiarization, CapabilitySpeakerIdentification, CapabilitySpeakerAttribution},
			Forbidden: []Capability{CapabilityToolCalling, CapabilityLLM, CapabilityRealtimeAudio, CapabilityTTS},
		},
		{
			Mode:         ModeAssist,
			Intelligence: IntelligenceUtility,
			Input:        "audio_or_text_with_optional_context",
			Output:       "one_shot_result",
			Allowed:      []Capability{CapabilityLLM, CapabilityToolCalling, CapabilityTTS, CapabilitySessionSummary, CapabilityPostSTTReplacements, CapabilitySpeakerDiarization, CapabilitySpeakerIdentification, CapabilitySpeakerAttribution},
			Forbidden:    []Capability{CapabilityRealtimeAudio},
		},
		{
			Mode:         ModeVoiceAgent,
			Intelligence: IntelligenceBrainstorming,
			Input:        "realtime_audio_dialogue",
			Output:       "dialogue_transcript_and_optional_summary",
			Allowed: []Capability{
				CapabilityRealtimeAudio,
				CapabilityPipelineFallback,
				CapabilityAudioInput,
				CapabilityTTS,
				CapabilitySessionSummary,
				CapabilityToolCalling,
				CapabilityWordsPrompt,
				CapabilityWordsNativeHints,
				CapabilityTranscript,
				CapabilityInterruptions,
				CapabilitySessionResume,
				CapabilityNativeContextPrompt,
				CapabilityNativeKeyterms,
				CapabilityLanguageHints,
				CapabilitySpeakerStreaming,
				CapabilityReasoningEffort,
				CapabilityTranslation,
				CapabilityTranscriptionOnly,
				CapabilityPrivacyRedaction,
				CapabilityVoiceFocus,
				CapabilityMedicalDomain,
				CapabilitySpeakerDiarization,
				CapabilitySpeakerIdentification,
				CapabilitySpeakerAttribution,
			},
			Forbidden: []Capability{CapabilityTranscription},
		},
		{
			Mode:         ModeTTS,
			Intelligence: IntelligenceVoiceOutput,
			Input:        "text",
			Output:       "audio",
			Allowed:      []Capability{CapabilityTTS},
			Forbidden:    []Capability{CapabilityTranscription, CapabilitySTT, CapabilityLLM, CapabilityRealtimeAudio, CapabilityToolCalling, CapabilityPipelineFallback, CapabilityDictionaryPrompt, CapabilityDictionaryNativeHints, CapabilityWordsPrompt, CapabilityWordsNativeHints, CapabilityPostSTTReplacements, CapabilitySessionSummary},
		},
	}
}

// NormalizeMode maps a mode string to its canonical [Mode], accepting legacy
// and alternative spellings ("dictate", "stt", "voiceAgent", "voice-agent",
// "realtime_voice", "speak", ...). Unknown and empty values yield ModeNone.
func NormalizeMode(mode Mode) Mode {
	switch strings.TrimSpace(string(mode)) {
	case "dictate", "dictation", "transcribe", "stt":
		return ModeDictation
	case "assist":
		return ModeAssist
	case "voice_agent", "voiceAgent", "voice-agent", "realtime_voice":
		return ModeVoiceAgent
	case "tts", "voice_output", "speak", "speech":
		return ModeTTS
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
	if mode == ModeTTS && (profile.HasCapability(CapabilityLLM) || profile.HasCapability(CapabilityTranscription) || profile.HasCapability(CapabilityRealtimeAudio)) {
		return fmt.Errorf("tts profile %q cannot expose LLM / STT / realtime capabilities", profile.ID)
	}
	return nil
}
