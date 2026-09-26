package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestTranscriptionWorkerTranscribesSegmentsInParallelAndDeliversCombinedTranscript(t *testing.T) {
	transcriber := newParallelSegmentTranscriber(2)
	observer := &recordingObserver{}
	output := &recordingOutput{}
	runner := NewTranscriptionRunner(transcriber, nil)

	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:     time.Second,
		QueueSize:   1,
		Runner:      runner,
		Output:      output,
		Transformer: replacingTranscriptTransformer{},
		Observer:    observer,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(speechkit.TranscriptionJob{
		Submission: speechkit.Submission{
			PCM:          []byte(strings.Repeat("x", 12800)),
			WAV:          []byte("full-session"),
			DurationSecs: 0.4,
			Language:     "de",
		},
		Segments: []speechkit.Submission{
			{WAV: []byte("segment-1"), DurationSecs: 0.2, Language: "de"},
			{WAV: []byte("segment-2"), DurationSecs: 0.2, Language: "de"},
		},
		Target: "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	select {
	case <-transcriber.started:
	case <-time.After(time.Second):
		t.Fatal("segments were not started in parallel")
	}
	if got := output.snapshot(); len(got) != 0 {
		t.Fatalf("delivered before all segment transcripts completed: %#v", got)
	}
	close(transcriber.release)
	worker.Close()
	worker.Wait()

	if got := transcriber.maxConcurrency(); got < 2 {
		t.Fatalf("max segment concurrency = %d, want at least 2", got)
	}
	delivered := output.snapshot()
	if len(delivered) != 1 {
		t.Fatalf("delivered outputs = %d, want 1", len(delivered))
	}
	if got, want := delivered[0].transcript.Text, "Kombify"; got != want {
		t.Fatalf("delivered transcript = %q, want %q", got, want)
	}
	if got, want := delivered[0].target, any("editor"); got != want {
		t.Fatalf("delivered target = %v, want %v", got, want)
	}
	if got := observer.committed; len(got) != 1 || got[0] != "Kombify" {
		t.Fatalf("observer committed = %#v", got)
	}
}

func TestTranscriptionWorkerHandlesTranscriberErrors(t *testing.T) {
	observer := &recordingObserver{}
	output := &recordingOutput{}
	runner := NewTranscriptionRunner(stubTranscriber{err: errors.New("boom")}, nil)

	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 1,
		Runner:    runner,
		Output:    output,
		Observer:  observer,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(speechkit.TranscriptionJob{
		Submission: speechkit.Submission{
			PCM:          []byte(strings.Repeat("a", 6400)),
			WAV:          []byte("wav"),
			DurationSecs: 0.2,
			Language:     "de",
		},
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	worker.Close()
	worker.Wait()

	if len(output.delivered) != 0 {
		t.Fatalf("delivered outputs = %d, want 0", len(output.delivered))
	}
	if observer.finalization.Recognition != speechkit.RecognitionFailed ||
		observer.finalization.Output != speechkit.OutputNotRequested ||
		observer.finalization.Persistence != speechkit.PersistenceNotRequested {
		t.Fatal("recognition failure was reported as completed output or history")
	}
	hasSTTError := false
	for _, log := range observer.logs {
		if strings.HasPrefix(log, "error:") {
			hasSTTError = true
			break
		}
	}
	if !hasSTTError {
		t.Fatalf("observer logs = %#v", observer.logs)
	}
}

func TestTranscriptionWorkerDeliversBeforeHistoryPersistence(t *testing.T) {
	store := newBlockingPersistence()
	output := &recordingOutput{}
	commitObserver := &testCommitObserver{}
	runner := NewTranscriptionRunner(stubTranscriber{
		transcript: speechkit.Transcript{
			Text:     "  fast dictation  ",
			Language: "en",
			Provider: "local",
			Model:    "ggml-large-v3-turbo.bin",
			Duration: 250 * time.Millisecond,
		},
	}, store).WithObserver(commitObserver)

	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 1,
		Runner:    runner,
		Output:    output,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(speechkit.TranscriptionJob{
		Submission: speechkit.Submission{
			WAV:          []byte("wav"),
			DurationSecs: 0.2,
			Language:     "en",
			Prefix:       "\n\n",
		},
		Target: "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	worker.Close()

	var delivered []deliveredTranscript
	deadline := time.After(time.Second)
	for {
		delivered = output.snapshot()
		if len(delivered) == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("output was not delivered before history persistence finished")
		case <-time.After(5 * time.Millisecond):
		}
	}

	if got, want := delivered[0].transcript.Text, "\n\nfast dictation"; got != want {
		t.Fatalf("delivered transcript = %q, want %q", got, want)
	}

	select {
	case <-store.started:
	case <-time.After(time.Second):
		t.Fatal("history persistence did not start")
	}

	close(store.release)
	worker.Wait()

	select {
	case <-store.done:
	default:
		t.Fatal("history persistence did not finish")
	}
	if len(commitObserver.completions) != 1 {
		t.Fatalf("commit observer completions = %d, want 1", len(commitObserver.completions))
	}
	if !commitObserver.completions[0].TranscriptionPersisted {
		t.Fatal("transcription persistence notification missing")
	}
}
