package speechkit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type stubTranscriber struct {
	transcript Transcript
	err        error
}

func (s stubTranscriber) Transcribe(_ context.Context, _ []byte, _ float64, _ string) (Transcript, error) {
	if s.err != nil {
		return Transcript{}, s.err
	}
	return s.transcript, nil
}

type deliveredTranscript struct {
	transcript Transcript
	target     any
}

type recordingOutput struct {
	mu        sync.Mutex
	delivered []deliveredTranscript
	err       error
}

func (o *recordingOutput) Deliver(_ context.Context, transcript Transcript, target any) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return o.err
	}
	o.delivered = append(o.delivered, deliveredTranscript{transcript: transcript, target: target})
	return nil
}

type recordingObserver struct {
	mu         sync.Mutex
	states     []string
	logs       []string
	committed  []string
	quickNotes []bool
}

func (o *recordingObserver) OnState(status, text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.states = append(o.states, status+":"+text)
}

func (o *recordingObserver) OnLog(message, kind string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.logs = append(o.logs, kind+":"+message)
}

func (o *recordingObserver) OnTranscriptCommitted(transcript Transcript, quickNote bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.committed = append(o.committed, transcript.Text)
	o.quickNotes = append(o.quickNotes, quickNote)
}

func TestTranscriptionWorkerProcessesJobs(t *testing.T) {
	observer := &recordingObserver{}
	output := &recordingOutput{}
	runner := NewTranscriptionRunner(stubTranscriber{
		transcript: Transcript{
			Text:     "hello world",
			Provider: "local",
			Duration: 1500 * time.Millisecond,
		},
	}, nil)

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

	if err := worker.Submit(TranscriptionJob{
		Submission: Submission{
			PCM:          []byte(strings.Repeat("a", 6400)),
			WAV:          []byte("wav"),
			DurationSecs: 0.2,
			Language:     "en",
			Prefix:       "\n\n",
			QuickNote:    true,
		},
		Target: "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	worker.Close()
	worker.Wait()

	if got := observer.committed; len(got) != 1 || got[0] != "\n\nhello world" {
		t.Fatalf("observer committed = %#v", got)
	}
	if got := observer.quickNotes; len(got) != 1 || !got[0] {
		t.Fatalf("observer quickNotes = %#v", got)
	}
	if got := observer.states; len(got) < 2 || got[0] != "processing:"+DefaultProcessingMessage || got[1] != "done:\n\nhello world" {
		t.Fatalf("observer states = %#v", got)
	}
	if len(output.delivered) != 1 {
		t.Fatalf("delivered outputs = %d, want 1", len(output.delivered))
	}
	if got, want := output.delivered[0].transcript.Text, "\n\nhello world"; got != want {
		t.Fatalf("delivered transcript = %q, want %q", got, want)
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

	if err := worker.Submit(TranscriptionJob{
		Submission: Submission{
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
	if got := observer.states; len(got) < 2 || got[0] != "processing:"+DefaultProcessingMessage || got[1] != "idle:" {
		t.Fatalf("observer states = %#v", got)
	}
	if got := observer.logs; len(got) < 2 || !strings.HasPrefix(got[1], "error:STT error: boom") {
		t.Fatalf("observer logs = %#v", got)
	}
}

func TestTranscriptionWorkerRequiresTranscriber(t *testing.T) {
	_, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Runner: NewTranscriptionRunner(nil, nil),
	})
	if !errors.Is(err, ErrMissingTranscriber) {
		t.Fatalf("NewTranscriptionWorker() error = %v, want %v", err, ErrMissingTranscriber)
	}
}
