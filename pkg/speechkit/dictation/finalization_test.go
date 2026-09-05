package dictation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/dictation"
)

type recorder struct{}

func (recorder) Start() error               { return nil }
func (recorder) Stop() ([]byte, error)      { return make([]byte, 6400), nil }
func (recorder) SetPCMHandler(func([]byte)) {}

type transcriber struct{ text string }

func (t transcriber) Transcribe(context.Context, []byte, float64, string) (speechkit.Transcript, error) {
	return speechkit.Transcript{Text: t.text, Language: "en"}, nil
}

type outputFunc func(context.Context, speechkit.Transcript, any) error

func (f outputFunc) Deliver(ctx context.Context, text speechkit.Transcript, target any) error {
	return f(ctx, text, target)
}

type history struct {
	speechkit.Persistence
	save func(context.Context, string) error
}

func (h history) SaveTranscription(ctx context.Context, text, _, _, _ string, _, _ int64, _ []byte) error {
	return h.save(ctx, text)
}

type observerFunc func(speechkit.Transcript, speechkit.TranscriptionFinalization, any)

func (f observerFunc) OnTranscriptionFinalized(t speechkit.Transcript, result speechkit.TranscriptionFinalization, target any) {
	f(t, result, target)
}

// Regression: Stop returned on output error before preserving recognized text.
func TestServicePreservesTextAfterOutputFailure(t *testing.T) {
	outputErr := errors.New("adapter failed")
	saveErr := errors.New("history failed")
	delivered, recognized, saved := false, false, false
	target := speechkit.TargetRef{Kind: speechkit.TargetKindEditor, ID: "original"}
	service, err := dictation.NewService(dictation.Options{
		Recorder: recorder{}, Transcriber: transcriber{text: "recoverable fixture"},
		Target: target,
		Observer: observerFunc(func(text speechkit.Transcript, f speechkit.TranscriptionFinalization, gotTarget any) {
			if f.Output == speechkit.OutputRequested {
				recognized = text.Text == "recoverable fixture" && gotTarget == target
			}
		}),
		Output: outputFunc(func(context.Context, speechkit.Transcript, any) error {
			if !recognized || saved {
				t.Fatal("recognition must be recoverable before output, and output before history")
			}
			delivered = true
			return outputErr
		}),
		Store: history{save: func(_ context.Context, text string) error {
			if !delivered || text != "recoverable fixture" {
				t.Fatal("history did not receive recognized text after output failure")
			}
			saved = true
			return saveErr
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	run, err := service.Stop(context.Background())
	if !saved || !errors.Is(err, outputErr) || !errors.Is(err, saveErr) {
		t.Fatalf("both independent failures must be returned: %v", err)
	}
	if run.Transcript.Text != "recoverable fixture" ||
		run.Finalization.Recognition != speechkit.RecognitionRecognized ||
		run.Finalization.Output != speechkit.OutputFailed ||
		run.Finalization.Persistence != speechkit.PersistenceFailed {
		t.Fatal("Stop lost the recoverable result or misreported completion")
	}
}

// Stable invariant: an empty recognition must never replace the target selection.
func TestServiceEmptyRecognitionDoesNotRequestOutput(t *testing.T) {
	service, err := dictation.NewService(dictation.Options{
		Recorder: recorder{}, Transcriber: transcriber{text: " \n"},
		Output: outputFunc(func(context.Context, speechkit.Transcript, any) error {
			t.Fatal("empty text reached output")
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	run, err := service.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if run.Finalization.Recognition != speechkit.RecognitionEmpty ||
		run.Finalization.Output != speechkit.OutputNotRequested ||
		run.Finalization.Persistence != speechkit.PersistenceNotRequested {
		t.Fatal("empty recognition was reported as delivered or stored")
	}
}
