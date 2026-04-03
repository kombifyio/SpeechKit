// Example: Using SpeechKit as a Go library for speech-to-text.
//
// This demonstrates how to use the SpeechKit framework without the
// desktop UI — just the transcription pipeline as a Go library.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// --- Step 1: Implement the Transcriber interface ---
// This is where you plug in your STT engine (Whisper, Google, etc.)

type exampleTranscriber struct{}

func (t *exampleTranscriber) Transcribe(ctx context.Context, audio []byte, durationSecs float64, language string) (speechkit.Transcript, error) {
	// Replace this with a real STT provider call.
	// SpeechKit ships providers in internal/stt/ that you can reference.
	return speechkit.Transcript{
		Text:     "[transcribed text would appear here]",
		Language: language,
		Duration: time.Duration(durationSecs * float64(time.Second)),
		Provider: "example",
		Model:    "example-v1",
	}, nil
}

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
	fmt.Printf("[result] %s (provider: %s, model: %s)\n", transcript.Text, transcript.Provider, transcript.Model)
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

	transcriber := &exampleTranscriber{}
	recorder := &exampleRecorder{}
	observer := &exampleObserver{}
	output := &exampleOutput{}

	// Create the transcription runner (handles commit + persistence).
	// Pass nil for store if you don't need persistence.
	runner := speechkit.NewTranscriptionRunner(transcriber, nil)

	// Create the transcription worker (async job queue).
	worker, err := speechkit.NewTranscriptionWorker(speechkit.TranscriptionWorkerConfig{
		Timeout:   30 * time.Second,
		QueueSize: 4,
		Runner:    runner,
		Output:    output,
		Observer:  observer,
	})
	if err != nil {
		log.Fatal(err)
	}
	worker.Start(ctx)
	defer worker.Close()

	// Create the recording controller.
	controller := speechkit.NewRecordingController(recorder, worker, observer, nil)

	// Simulate a recording session.
	fmt.Println("SpeechKit Library Example")
	fmt.Println("Press Ctrl+C to exit.")
	fmt.Println()

	if err := controller.Start(speechkit.RecordingStartOptions{Language: "en"}); err != nil {
		log.Fatal(err)
	}

	// In a real app, the user would speak and then stop recording.
	// Here we simulate a short recording.
	time.Sleep(2 * time.Second)

	if err := controller.Stop(speechkit.RecordingStopOptions{}); err != nil {
		log.Fatal(err)
	}

	// Wait for transcription to complete.
	worker.Wait()
	fmt.Println("Done.")
}
