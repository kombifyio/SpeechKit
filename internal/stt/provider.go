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
	"time"
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
	Language string // "de", "en", "auto"
	Model    string // Optional: model override
	Prompt   string // Optional: provider-specific hint prompt for better recognition
}

// Result holds the output of a transcription.
type Result struct {
	Text       string
	Language   string
	Duration   time.Duration
	Provider   string
	Model      string
	Confidence float64 // If available from the provider
}
