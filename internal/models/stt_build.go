package models

import (
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/allproviders"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/deepgram"
)

// STTBuildSpec carries the inputs needed to construct a cloud STT provider
// for a given host ExecutionMode. It mirrors allproviders.BuildSpec but is
// typed with the host's ExecutionMode so config-derived call sites stay
// in the host vocabulary.
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
	Deepgram deepgram.Options
	// Google streaming credential env-var names (optional), forwarded to the
	// opt-in Google STT provider so realtime transcription can authenticate.
	GoogleStreamingCredentialsEnv   string
	GoogleApplicationCredentialsEnv string
	// FoundrySpeech selects the Azure Speech fast-transcription surface of a
	// Foundry resource (optional).
	FoundrySpeech *allproviders.FoundrySpeechOpts
	// BearerToken supplies Entra bearer tokens for Foundry-hosted providers.
	BearerToken framework.BearerTokenFunc
}

// BuildSTT constructs the provider described by spec through the public
// registry and returns its canonical name alongside it.
func BuildSTT(spec STTBuildSpec) (string, stt.STTProvider, error) {
	return allproviders.Build(allproviders.BuildSpec{
		ExecutionMode:    spec.ExecutionMode,
		Provider:         spec.Provider,
		ModelID:          spec.ModelID,
		APIKey:           spec.APIKey,
		Token:            spec.Token,
		BaseURL:          spec.BaseURL,
		DiarizationModel: spec.DiarizationModel,
		Deepgram:         spec.Deepgram,
		FoundrySpeech:    spec.FoundrySpeech,
		BearerToken:      spec.BearerToken,

		GoogleStreamingCredentialsEnv:   spec.GoogleStreamingCredentialsEnv,
		GoogleApplicationCredentialsEnv: spec.GoogleApplicationCredentialsEnv,
	})
}
