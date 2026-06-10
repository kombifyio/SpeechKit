package stt

import (
	"fmt"

	"github.com/kombifyio/SpeechKit/internal/models"
)

// BuildSpec carries the inputs needed to construct a cloud STT provider for a
// given ExecutionMode. The host config layer resolves secrets and passes them
// in; the registry owns the provider's canonical name, endpoint, and
// constructor — the mapping that was previously duplicated across call sites.
type BuildSpec struct {
	ExecutionMode models.ExecutionMode
	ModelID       string

	APIKey  string // cloud API key (OpenAI/Groq/Google/Deepgram/AssemblyAI/OpenRouter)
	Token   string // HuggingFace token
	BaseURL string // Ollama base URL (optional; defaulted when empty)

	// DiarizationModel overrides the Deepgram diarization model (optional).
	DiarizationModel string
	// Deepgram forwards provider-specific Listen options (optional).
	Deepgram DeepgramOptions
	// Google streaming credential env-var names (optional), forwarded to the
	// Google provider so realtime transcription can authenticate.
	GoogleStreamingCredentialsEnv   string
	GoogleApplicationCredentialsEnv string
}

// Build constructs the cloud STT provider for spec.ExecutionMode and returns
// its canonical Name plus the provider. It is the single source of truth for
// the ExecutionMode → (name, endpoint, constructor) mapping.
//
// ExecutionModeLocal is host-managed (whisper.cpp subprocess lifecycle) and is
// intentionally not handled here.
func Build(spec BuildSpec) (string, STTProvider, error) {
	switch spec.ExecutionMode {
	case models.ExecutionModeHFRouted:
		return "huggingface", NewHuggingFaceProvider(spec.ModelID, spec.Token), nil
	case models.ExecutionModeOpenAI:
		return "openai", NewOpenAICompatibleProvider("openai", "https://api.openai.com", spec.APIKey, spec.ModelID), nil
	case models.ExecutionModeGroq:
		return "groq", NewOpenAICompatibleProvider("groq", "https://api.groq.com/openai", spec.APIKey, spec.ModelID), nil
	case models.ExecutionModeGoogle:
		provider := NewGoogleSTTProvider(spec.APIKey, spec.ModelID)
		provider.SetStreamingCredentialEnvs(spec.GoogleStreamingCredentialsEnv, spec.GoogleApplicationCredentialsEnv)
		return "google", provider, nil
	case models.ExecutionModeDeepgram:
		provider := NewDeepgramProvider(spec.APIKey, spec.ModelID)
		if spec.DiarizationModel != "" {
			provider.DiarizationModel = spec.DiarizationModel
		}
		if hasDeepgramOptions(spec.Deepgram) {
			provider.ApplyOptions(spec.Deepgram)
		}
		return "deepgram", provider, nil
	case models.ExecutionModeAssemblyAI:
		return "assemblyai", NewAssemblyAIProvider(spec.APIKey, spec.ModelID), nil
	case models.ExecutionModeOpenRouter:
		return "openrouter", NewOpenRouterSTTProvider(spec.APIKey, spec.ModelID), nil
	case models.ExecutionModeOllama:
		baseURL := spec.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		return "ollama", NewOllamaSTTProvider(baseURL, spec.ModelID), nil
	default:
		return "", nil, fmt.Errorf("stt: unsupported execution mode %q", spec.ExecutionMode)
	}
}

func hasDeepgramOptions(opts DeepgramOptions) bool {
	return opts.Configured ||
		opts.SmartFormat ||
		opts.Dictation ||
		opts.FillerWords ||
		opts.Numerals ||
		opts.DetectLanguage ||
		opts.UseVocabularyKeyterms ||
		opts.LanguageOverride != "" ||
		len(opts.Keyterms) > 0 ||
		opts.EndpointingMs != 0
}
