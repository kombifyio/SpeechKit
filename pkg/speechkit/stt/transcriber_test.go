package stt_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// fakeProvider records the TranscribeOpts it received and returns a canned
// result or error.
type fakeProvider struct {
	gotOpts  stt.TranscribeOpts
	gotAudio []byte
	result   *stt.Result
	err      error
}

func (f *fakeProvider) Transcribe(_ context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	f.gotAudio = audio
	f.gotOpts = opts
	return f.result, f.err
}

func (f *fakeProvider) Name() string                   { return "fake" }
func (f *fakeProvider) Health(_ context.Context) error { return nil }

func TestAsTranscriberPerCallLanguageWinsOverBase(t *testing.T) {
	provider := &fakeProvider{result: &stt.Result{Text: "hi"}}
	transcriber := stt.AsTranscriber(provider, stt.WithTranscribeOpts(stt.TranscribeOpts{
		Language: "de",
		Prompt:   "domain prompt",
	}))

	if _, err := transcriber.Transcribe(context.Background(), []byte("audio"), 1.5, "multi"); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if provider.gotOpts.Language != "multi" {
		t.Fatalf("per-call language should win over base, got %q", provider.gotOpts.Language)
	}
	if provider.gotOpts.Prompt != "domain prompt" {
		t.Fatalf("base options should carry through, got prompt %q", provider.gotOpts.Prompt)
	}
	if string(provider.gotAudio) != "audio" {
		t.Fatalf("audio should pass through unchanged, got %q", provider.gotAudio)
	}
}

func TestAsTranscriberEmptyLanguageStaysEmpty(t *testing.T) {
	provider := &fakeProvider{result: &stt.Result{Text: "hi"}}
	transcriber := stt.AsTranscriber(provider)

	if _, err := transcriber.Transcribe(context.Background(), []byte("audio"), 1.5, ""); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if provider.gotOpts.Language != "" {
		t.Fatalf("adapter must not invent a language, got %q", provider.gotOpts.Language)
	}
}

func TestAsTranscriberBaseLanguageFillsEmptyCall(t *testing.T) {
	provider := &fakeProvider{result: &stt.Result{Text: "hi"}}
	transcriber := stt.AsTranscriber(provider, stt.WithTranscribeOpts(stt.TranscribeOpts{Language: "multi"}))

	if _, err := transcriber.Transcribe(context.Background(), []byte("audio"), 1.5, ""); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if provider.gotOpts.Language != "multi" {
		t.Fatalf("base language should apply when per-call language is empty, got %q", provider.gotOpts.Language)
	}
}

func TestAsTranscriberMapsFullResult(t *testing.T) {
	speakers := &speaker.DiarizationResult{}
	provider := &fakeProvider{result: &stt.Result{
		Text:       "hello world",
		Language:   "en",
		Duration:   1234 * time.Millisecond,
		Provider:   "deepgram",
		Model:      "nova-3",
		Confidence: 0.97,
		Words: []stt.WordConfidence{
			{Text: "hello", Confidence: 0.99, StartMs: 0, EndMs: 400},
			{Text: "world", Confidence: 0.42, StartMs: 410, EndMs: 900},
		},
		Speakers: speakers,
	}}
	transcriber := stt.AsTranscriber(provider)

	got, err := transcriber.Transcribe(context.Background(), []byte("audio"), 2.0, "en")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	want := speechkit.Transcript{
		Text:       "hello world",
		Language:   "en",
		Duration:   1234 * time.Millisecond,
		Provider:   "deepgram",
		Model:      "nova-3",
		Confidence: 0.97,
		Words: []speechkit.WordConfidence{
			{Text: "hello", Confidence: 0.99, StartMs: 0, EndMs: 400},
			{Text: "world", Confidence: 0.42, StartMs: 410, EndMs: 900},
		},
		Speakers: speakers,
	}
	if got.Text != want.Text || got.Language != want.Language || got.Duration != want.Duration ||
		got.Provider != want.Provider || got.Model != want.Model || got.Confidence != want.Confidence {
		t.Fatalf("transcript mismatch:\n got %+v\nwant %+v", got, want)
	}
	if len(got.Words) != len(want.Words) {
		t.Fatalf("word count mismatch: got %d want %d", len(got.Words), len(want.Words))
	}
	for i := range want.Words {
		if got.Words[i] != want.Words[i] {
			t.Fatalf("word %d mismatch: got %+v want %+v", i, got.Words[i], want.Words[i])
		}
	}
	if got.Speakers != speakers {
		t.Fatalf("speakers should pass through unchanged")
	}
}

func TestAsTranscriberDurationFallsBackToCallerMeasurement(t *testing.T) {
	provider := &fakeProvider{result: &stt.Result{Text: "hi"}}
	transcriber := stt.AsTranscriber(provider)

	got, err := transcriber.Transcribe(context.Background(), []byte("audio"), 2.5, "")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if want := 2500 * time.Millisecond; got.Duration != want {
		t.Fatalf("duration fallback: got %v want %v", got.Duration, want)
	}
}

func TestAsTranscriberPropagatesProviderError(t *testing.T) {
	providerErr := errors.New("provider unavailable")
	provider := &fakeProvider{err: providerErr}
	transcriber := stt.AsTranscriber(provider)

	_, err := transcriber.Transcribe(context.Background(), []byte("audio"), 1.0, "")
	if !errors.Is(err, providerErr) {
		t.Fatalf("expected provider error to propagate, got %v", err)
	}
}

func TestToTranscriptNilResultYieldsZeroTranscript(t *testing.T) {
	if got := stt.ToTranscript(nil, 3.0); !reflect.DeepEqual(got, speechkit.Transcript{}) {
		t.Fatalf("nil result should yield zero transcript, got %+v", got)
	}
}
