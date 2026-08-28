// Package openaicompat is the OpenAI-compatible transcription provider for
// SpeechKit. One adapter covers every server that speaks the OpenAI audio
// transcription API: OpenAI itself, Groq, Ollama, and self-hosted
// whisper-server deployments (see pkg/speechkit/stt/vps).
//
// In v0.64.0 the identifiers below alias the implementation that still lives
// in pkg/speechkit/stt. v0.65.0 moves the implementation here and deletes the
// old names, so code written against this package needs no further change.
package openaicompat

//lint:file-ignore SA1019 This package exists to alias the names deprecated in
// pkg/speechkit/stt; flagging its own aliases would defeat the migration window.

import "github.com/kombifyio/SpeechKit/pkg/speechkit/stt"

// Provider transcribes through any OpenAI-compatible audio endpoint.
type Provider = stt.OpenAICompatibleProvider

// New returns a provider for an arbitrary OpenAI-compatible endpoint. name is
// the provider identity reported on results.
func New(name, baseURL, apiKey, model string) *Provider {
	return stt.NewOpenAICompatibleProvider(name, baseURL, apiKey, model)
}

// NewOpenAI returns the OpenAI provider on its default endpoint and model.
func NewOpenAI(apiKey string) *Provider { return stt.NewOpenAISTTProvider(apiKey) }

// NewGroq returns the Groq provider on its default endpoint and model.
func NewGroq(apiKey string) *Provider { return stt.NewGroqSTTProvider(apiKey) }

// NewOllama returns a provider for a local Ollama server. An empty model uses
// the provider default.
func NewOllama(baseURL, model string) *Provider { return stt.NewOllamaSTTProvider(baseURL, model) }
