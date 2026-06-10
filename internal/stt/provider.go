// Package stt defines the SpeechKit speech-to-text provider interface and
// houses the concrete provider implementations: whisper.cpp (local
// built-in), HuggingFace, OpenAI, Groq, Google, an OpenAI-compatible
// adapter (covers Ollama and other compatible servers), and the
// self-hosted VPS adapter.
//
// All providers must go through [github.com/kombifyio/SpeechKit/internal/netsec]
// for outbound HTTP. Routing — which provider to pick for a given
// request — lives in [github.com/kombifyio/SpeechKit/internal/router].
package stt

import (
	"context"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// STTProvider defines the interface for all speech-to-text backends.
type STTProvider interface {
	// Transcribe sends audio data to the STT backend and returns the transcription.
	Transcribe(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error)

	// Name returns the provider identifier (e.g. "local", "vps", "huggingface").
	Name() string

	// Health checks if the provider is reachable and ready.
	Health(ctx context.Context) error
}

// TranscribeOpts configures a single transcription request.
type TranscribeOpts struct {
	Language                  string                         // "de", "en", "auto"; request override only
	Model                     string                         // Optional: model override
	Prompt                    string                         // Optional: provider-specific hint prompt for better recognition
	Keyterms                  []string                       // Optional: provider-native vocabulary bias terms
	Speaker                   speaker.Options                // Optional speaker diarization / attribution request
	Options                   provideropts.Values            // Optional normalized global/default voice options
	ProviderOptions           provideropts.Values            // Optional normalized overrides for the selected provider
	ProviderOptionsByProvider map[string]provideropts.Values // Optional provider-keyed overrides used by routers
}

func (o TranscribeOpts) ForProvider(provider string) TranscribeOpts {
	provider = normalizeProviderKey(provider)
	if len(o.ProviderOptionsByProvider) == 0 {
		o.ProviderOptions = o.ProviderOptions.Clone()
		return o
	}
	values := o.ProviderOptions.Clone()
	values = values.Merge(o.ProviderOptionsByProvider[provider])
	o.ProviderOptions = values
	return o
}

func normalizeProviderKey(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

// Result holds the output of a transcription.
type Result struct {
	Text       string
	Language   string
	Duration   time.Duration
	Provider   string
	Model      string
	Confidence float64 // If available from the provider
	Speakers   *speaker.DiarizationResult
}
