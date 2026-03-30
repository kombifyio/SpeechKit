package tts

import (
	"context"
	"time"
)

// Provider defines the interface for all text-to-speech backends.
type Provider interface {
	// Synthesize converts text to audio bytes.
	Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (*Result, error)

	// Name returns the provider identifier (e.g. "openai", "google", "kokoro").
	Name() string

	// Health checks if the provider is reachable and ready.
	Health(ctx context.Context) error
}

// SynthesizeOpts configures a single TTS request.
type SynthesizeOpts struct {
	Locale string  // "de-DE", "en-US", "auto"
	Voice  string  // Provider-specific voice ID; empty = default
	Speed  float64 // 0.25 - 4.0, default 1.0
	Format string  // "wav", "mp3", "opus", "pcm"; default "mp3"
}

// Result holds the output of a TTS synthesis.
type Result struct {
	Audio      []byte
	Format     string        // Actual format of the audio data
	SampleRate int           // Sample rate in Hz (e.g. 24000)
	Duration   time.Duration // Estimated duration of the audio
	Provider   string
	Voice      string
}
