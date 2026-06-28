package stt

import (
	"fmt"

	"github.com/kombifyio/SpeechKit/internal/models"
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// BuildSpec carries the inputs needed to construct a cloud STT provider for a
// given ExecutionMode. The host config layer resolves secrets and passes them
// in; the registry owns the provider's canonical name, endpoint, and
// constructor — the mapping that was previously duplicated across call sites.
type BuildSpec struct {
	ExecutionMode models.ExecutionMode
	Provider      string
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
	providerID := framework.NormalizeProviderID(spec.Provider)
	if providerID == "" {
		providerID = providerIDForExecutionMode(spec.ExecutionMode)
	}
	descriptor, ok := providerRegistry[providerID]
	if !ok {
		return "", nil, fmt.Errorf("stt: unsupported provider %q for execution mode %q", providerID, spec.ExecutionMode)
	}
	provider, err := descriptor.Build(spec)
	if err != nil {
		return "", nil, err
	}
	return descriptor.Name, provider, nil
}

type providerDescriptor struct {
	Name  string
	Build func(BuildSpec) (STTProvider, error)
}

var providerRegistry = map[string]providerDescriptor{
	"huggingface": {
		Name: "huggingface",
		Build: func(spec BuildSpec) (STTProvider, error) {
			return NewHuggingFaceProvider(spec.ModelID, spec.Token), nil
		},
	},
	"openai": {
		Name: "openai",
		Build: func(spec BuildSpec) (STTProvider, error) {
			return NewOpenAICompatibleProvider("openai", "https://api.openai.com", spec.APIKey, spec.ModelID), nil
		},
	},
	"groq": {
		Name: "groq",
		Build: func(spec BuildSpec) (STTProvider, error) {
			return NewOpenAICompatibleProvider("groq", "https://api.groq.com/openai", spec.APIKey, spec.ModelID), nil
		},
	},
	"google": {
		Name: "google",
		Build: func(spec BuildSpec) (STTProvider, error) {
			provider := NewGoogleSTTProvider(spec.APIKey, spec.ModelID)
			provider.SetStreamingCredentialEnvs(spec.GoogleStreamingCredentialsEnv, spec.GoogleApplicationCredentialsEnv)
			return provider, nil
		},
	},
	"deepgram": {
		Name: "deepgram",
		Build: func(spec BuildSpec) (STTProvider, error) {
			provider := NewDeepgramProvider(spec.APIKey, spec.ModelID)
			if spec.DiarizationModel != "" {
				provider.DiarizationModel = spec.DiarizationModel
			}
			if hasDeepgramOptions(spec.Deepgram) {
				provider.ApplyOptions(spec.Deepgram)
			}
			return provider, nil
		},
	},
	"assemblyai": {
		Name: "assemblyai",
		Build: func(spec BuildSpec) (STTProvider, error) {
			return NewAssemblyAIProvider(spec.APIKey, spec.ModelID), nil
		},
	},
	"openrouter": {
		Name: "openrouter",
		Build: func(spec BuildSpec) (STTProvider, error) {
			return NewOpenRouterSTTProvider(spec.APIKey, spec.ModelID), nil
		},
	},
	"ollama": {
		Name: "ollama",
		Build: func(spec BuildSpec) (STTProvider, error) {
			baseURL := spec.BaseURL
			if baseURL == "" {
				baseURL = "http://localhost:11434"
			}
			return NewOllamaSTTProvider(baseURL, spec.ModelID), nil
		},
	},
}

func providerIDForExecutionMode(mode models.ExecutionMode) string {
	return framework.ProviderIDForExecutionMode(framework.ExecutionMode(mode))
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
