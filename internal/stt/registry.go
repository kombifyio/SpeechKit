package stt

// The STT provider registry now lives in the public pkg/speechkit/stt package
// (Build/Register with a speechkit.ExecutionMode-typed BuildSpec). This file
// keeps the host-facing BuildSpec, which is typed with the host's internal
// models.ExecutionMode, and forwards to the public registry so existing call
// sites (cmd/speechkit/model_profiles.go) keep compiling unchanged.

import (
	"github.com/kombifyio/SpeechKit/internal/models"
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	pkgstt "github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// BuildSpec carries the inputs needed to construct a cloud STT provider for a
// given ExecutionMode. It mirrors pkg/speechkit/stt.BuildSpec but keeps the
// internal models.ExecutionMode type for host callers.
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

// Build forwards to the public pkg/speechkit/stt registry, converting the
// internal ExecutionMode to the framework enum.
func Build(spec BuildSpec) (string, STTProvider, error) {
	return pkgstt.Build(pkgstt.BuildSpec{
		ExecutionMode:                   framework.ExecutionMode(spec.ExecutionMode),
		Provider:                        spec.Provider,
		ModelID:                         spec.ModelID,
		APIKey:                          spec.APIKey,
		Token:                           spec.Token,
		BaseURL:                         spec.BaseURL,
		DiarizationModel:                spec.DiarizationModel,
		Deepgram:                        spec.Deepgram,
		GoogleStreamingCredentialsEnv:   spec.GoogleStreamingCredentialsEnv,
		GoogleApplicationCredentialsEnv: spec.GoogleApplicationCredentialsEnv,
	})
}
