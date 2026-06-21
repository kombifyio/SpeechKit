// Package stt defines the SpeechKit speech-to-text provider interface and
// houses the concrete provider implementations: whisper.cpp (local
// built-in), HuggingFace, OpenAI, Groq, Google, an OpenAI-compatible
// adapter (covers Ollama and other compatible servers), and the
// self-hosted VPS adapter.
//
// All providers must go through [github.com/kombifyio/SpeechKit/pkg/speechkit/netsec]
// for outbound HTTP. Routing — which provider to pick for a given request — is
// the host's responsibility (a routing/fallback layer above these providers).
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

// WordConfidence is a single recognized word with the provider's per-word
// acoustic confidence in [0,1]. It is populated only by providers that expose
// word-level confidence (Deepgram, AssemblyAI); it stays nil for providers
// that do not (Google v1, OpenAI/Groq/HuggingFace Whisper, local whisper.cpp).
// Confidence here is acoustic (how sure the model is it heard this word), not
// semantic — a low value flags a likely mis-recognition or dropped word.
type WordConfidence struct {
	Text       string
	Confidence float64
	StartMs    int64
	EndMs      int64
}

// Result holds the output of a transcription.
type Result struct {
	Text       string
	Language   string
	Duration   time.Duration
	Provider   string
	Model      string
	Confidence float64 // If available from the provider
	// Words carries per-word acoustic confidence when the provider exposes it.
	// nil for providers without word-level confidence. Offsets are NOT tracked
	// because downstream vocabulary/punctuation rewriting invalidates them;
	// consumers match on the word text instead.
	Words    []WordConfidence
	Speakers *speaker.DiarizationResult
}
