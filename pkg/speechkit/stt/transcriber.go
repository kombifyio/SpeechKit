package stt

import (
	"context"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// TranscriberOption configures the adapter returned by [AsTranscriber].
type TranscriberOption func(*transcriberOptions)

type transcriberOptions struct {
	base TranscribeOpts
}

// WithTranscribeOpts sets the base [TranscribeOpts] applied to every request
// the adapter makes (prompt, keyterms, provider options, and so on). The
// Language field of the base is used only when the per-call language passed to
// Transcribe is empty; a non-empty per-call language always wins.
func WithTranscribeOpts(base TranscribeOpts) TranscriberOption {
	return func(o *transcriberOptions) {
		o.base = base
	}
}

// AsTranscriber adapts an [STTProvider] to the kernel's
// [speechkit.Transcriber] interface, so hosts can hand any provider straight
// to dictation.NewRuntime or a TranscriptionWorker without hand-writing the
// mechanical Result-to-Transcript adapter. The per-call language overrides the
// base option language when non-empty; when both are empty the request carries
// no language at all — the adapter never invents a default, because language
// selection (including multilanguage) is an explicit caller decision.
//
// The device app keeps its own richer internal adapter (vocabulary bias,
// customization, routing); this bridge covers the plain provider-to-kernel
// path for library consumers.
func AsTranscriber(p STTProvider, opts ...TranscriberOption) speechkit.Transcriber {
	options := transcriberOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	return providerTranscriber{provider: p, base: options.base}
}

type providerTranscriber struct {
	provider STTProvider
	base     TranscribeOpts
}

func (t providerTranscriber) Transcribe(ctx context.Context, audio []byte, durationSecs float64, language string) (speechkit.Transcript, error) {
	reqOpts := t.base
	if language != "" {
		reqOpts.Language = language
	}
	result, err := t.provider.Transcribe(ctx, audio, reqOpts)
	if err != nil {
		return speechkit.Transcript{}, err
	}
	return ToTranscript(result, durationSecs), nil
}

// ToTranscript maps a provider [Result] to a [speechkit.Transcript]. The
// mapping is mechanical: text, language, provider, model, confidence, per-word
// confidences, and speaker diarization pass through 1:1. Duration comes from
// the Result when the provider reported one; otherwise durationSecs (the
// caller-measured audio length) fills in. A nil Result yields a zero
// Transcript.
func ToTranscript(r *Result, durationSecs float64) speechkit.Transcript {
	if r == nil {
		return speechkit.Transcript{}
	}
	duration := r.Duration
	if duration <= 0 {
		duration = time.Duration(durationSecs * float64(time.Second))
	}
	return speechkit.Transcript{
		Text:       r.Text,
		Language:   r.Language,
		Duration:   duration,
		Provider:   r.Provider,
		Model:      r.Model,
		Confidence: r.Confidence,
		Words:      transcriptWordConfidences(r.Words),
		Speakers:   r.Speakers,
	}
}

// transcriptWordConfidences copies the per-word slice so the Transcript never
// aliases the provider's buffer. nil in, nil out — providers without
// word-level confidence keep the Transcript field nil.
func transcriptWordConfidences(words []WordConfidence) []speechkit.WordConfidence {
	if len(words) == 0 {
		return nil
	}
	return append([]speechkit.WordConfidence(nil), words...)
}
