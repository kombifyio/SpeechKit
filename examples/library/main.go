// Example: Using SpeechKit as a Go library for speech-to-text.
//
// This demonstrates how to use the SpeechKit framework without the
// desktop UI — just the transcription pipeline as a Go library.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/dictation"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// --- Step 1: Pick an STT provider ---
// Any stt.STTProvider (the framework ships whisper.cpp, OpenAI, Groq,
// Deepgram, Google, and more) plugs into the runtime via stt.AsTranscriber —
// no hand-written adapter needed. This example uses a tiny fake provider so
// it runs without credentials; swap it for a real one, e.g.
// stt.NewOpenAICompatibleProvider(...).

type exampleProvider struct{}

func (p *exampleProvider) Transcribe(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	return &stt.Result{
		Text:     "[transcribed text would appear here]",
		Language: opts.Language,
		Provider: "example",
		Model:    "example-v1",
	}, nil
}

func (p *exampleProvider) Name() string { return "example" }

func (p *exampleProvider) Health(ctx context.Context) error { return nil }

// --- Step 2: Implement the AudioRecorder interface ---
// Captures audio from the microphone or another source.

type exampleRecorder struct {
	recording bool
	pcm       []byte
}

func (r *exampleRecorder) Start() error {
	r.recording = true
	r.pcm = nil
	fmt.Println("Recording started...")
	return nil
}

func (r *exampleRecorder) Stop() ([]byte, error) {
	r.recording = false
	fmt.Println("Recording stopped.")
	// Return captured PCM audio (16kHz, 16-bit, mono).
	// In production, use malgo or another audio library.
	if len(r.pcm) == 0 {
		r.pcm = []byte(strings.Repeat("a", 6400))
	}
	return r.pcm, nil
}

func (r *exampleRecorder) SetPCMHandler(handler func([]byte)) {
	// Called with PCM chunks during recording for live processing (e.g. VAD).
	// Can be a no-op if you don't need real-time audio access.
}

// --- Step 3: Implement the observer (status callbacks) ---

type exampleObserver struct{}

func (o *exampleObserver) OnState(status, text string) {
	fmt.Printf("[state] %s: %s\n", status, text)
}

func (o *exampleObserver) OnLog(message, kind string) {
	fmt.Printf("[log/%s] %s\n", kind, message)
}

func (o *exampleObserver) OnTranscriptCommitted(transcript speechkit.Transcript, quickNote bool) {
	label := "result"
	if quickNote {
		label = "quick-note"
	}
	fmt.Printf("[%s] %s (provider: %s, model: %s)\n", label, transcript.Text, transcript.Provider, transcript.Model)
}

// --- Step 4: Implement output delivery ---

type exampleOutput struct{}

func (o *exampleOutput) Deliver(ctx context.Context, transcript speechkit.Transcript, target any) error {
	fmt.Printf("\nTranscription: %s\n", transcript.Text)
	return nil
}

// --- Step 5: Wire it all together ---

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// stt.AsTranscriber bridges any STT provider to the runtime's
	// speechkit.Transcriber interface.
	transcriber := stt.AsTranscriber(&exampleProvider{})
	recorder := &exampleRecorder{}
	observer := &exampleObserver{}
	output := &exampleOutput{}

	runtime, err := dictation.NewRuntime(dictation.Options{
		Recorder:    recorder,
		Transcriber: transcriber,
		Output:      output,
		Language:    "en",
		Policy: speechkit.RuntimePolicy{
			EnabledModes: []speechkit.Mode{speechkit.ModeDictation},
			FixedProfiles: map[speechkit.Mode]string{
				speechkit.ModeDictation: "stt.openai.whisper-1",
			},
		},
	})
	if err != nil {
		slog.Error("dictation runtime init failed", "err", err)
		cancel()
		os.Exit(1) //nolint:gocritic // exitAfterDefer: cancel() called explicitly above before exit
	}

	// Simulate a recording session.
	fmt.Println("SpeechKit Library Example")
	fmt.Println("Press Ctrl+C to exit.")
	fmt.Println()

	observer.OnState("recording", "Speak now")
	if err := runtime.Start(ctx); err != nil {
		slog.Error("recording start failed", "err", err)
		os.Exit(1)
	}

	// In a real app, the user would speak and then stop recording.
	// Here we simulate a short recording.
	time.Sleep(2 * time.Second)

	run, err := runtime.Stop(ctx)
	if err != nil {
		slog.Error("recording stop failed", "err", err)
		os.Exit(1)
	}
	observer.OnTranscriptCommitted(run.Transcript, false)
	fmt.Println("Done.")
}
