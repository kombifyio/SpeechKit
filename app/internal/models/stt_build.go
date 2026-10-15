package models

import (
	"errors"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// ErrNoSTTBuilder reports that a cloud STT provider was requested without an
// STTBuilder. Composition roots inject the builder that matches the providers
// they ship; this package never links a provider set of its own, so a missing
// builder fails closed instead of silently producing no provider.
var ErrNoSTTBuilder = errors.New("models: no STT builder configured")

// DeepgramTuning carries the Deepgram Listen options as plain values, so the
// spec does not pull the Deepgram provider package into every consumer. The
// builder that knows Deepgram converts it.
type DeepgramTuning struct {
	Configured            bool
	SmartFormat           bool
	Dictation             bool
	FillerWords           bool
	Numerals              bool
	DetectLanguage        bool
	LanguageOverride      string
	UseVocabularyKeyterms bool
	Keyterms              []string
	EndpointingMs         int
}

// FoundrySpeechOpts selects the Azure Speech fast-transcription surface of a
// Foundry resource (MAI-Transcribe) instead of the OpenAI-compatible route.
type FoundrySpeechOpts struct {
	APIKey string
	// BearerToken, when set, wins over APIKey (Entra sign-in on Foundry).
	BearerToken framework.BearerTokenFunc
	Host        string
	Model       string
	Style       string // "clean" (default) or "verbatim"
	Diarization bool
}

// STTBuildSpec carries the inputs needed to construct a cloud STT provider
// for a given host ExecutionMode. It is made of plain values only: which
// provider implementations can honour it is the injected STTBuilder's concern.
type STTBuildSpec struct {
	ExecutionMode ExecutionMode
	Provider      string
	ModelID       string

	APIKey  string // cloud API key (OpenAI/Groq/Google/Deepgram/AssemblyAI/OpenRouter)
	Token   string // HuggingFace token
	BaseURL string // Ollama base URL (optional; defaulted when empty)

	// DiarizationModel overrides the Deepgram diarization model (optional).
	DiarizationModel string
	// Deepgram forwards provider-specific Listen options (optional).
	Deepgram DeepgramTuning
	// Google streaming credential env-var names (optional), forwarded to the
	// opt-in Google STT provider so realtime transcription can authenticate.
	GoogleStreamingCredentialsEnv   string
	GoogleApplicationCredentialsEnv string
	// GoogleRegion is the Speech-to-Text v2 multi-region for chirp_3.
	GoogleRegion string
	// FoundrySpeech selects the Azure Speech fast-transcription surface of a
	// Foundry resource (optional).
	FoundrySpeech *FoundrySpeechOpts
	// BearerToken supplies Entra bearer tokens for Foundry-hosted providers.
	BearerToken framework.BearerTokenFunc
}

// STTBuilder turns an STTBuildSpec into a provider and its canonical name.
// Implementations decide which providers they can build and return an error
// for the rest.
type STTBuilder interface {
	Build(spec STTBuildSpec) (name string, provider stt.STTProvider, err error)
}

// STTBuilderFunc adapts a function to STTBuilder.
type STTBuilderFunc func(spec STTBuildSpec) (string, stt.STTProvider, error)

// Build implements STTBuilder.
func (f STTBuilderFunc) Build(spec STTBuildSpec) (string, stt.STTProvider, error) {
	return f(spec)
}

// BuildSTT constructs the provider described by spec through builder and
// returns its canonical name alongside it. A nil builder yields
// ErrNoSTTBuilder.
func BuildSTT(builder STTBuilder, spec STTBuildSpec) (string, stt.STTProvider, error) {
	if builder == nil {
		return "", nil, ErrNoSTTBuilder
	}
	return builder.Build(spec)
}
