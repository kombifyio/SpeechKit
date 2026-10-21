package pipeline

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestTranscriptionWorkerRequiresTranscriber(t *testing.T) {
	_, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Runner: NewTranscriptionRunner(nil, nil),
	})
	if !errors.Is(err, ErrMissingTranscriber) {
		t.Fatalf("NewTranscriptionWorker() error = %v, want %v", err, ErrMissingTranscriber)
	}
}

func TestTranscriptionTimeoutForDurationScalesBeyondDefault(t *testing.T) {
	timeout := transcriptionTimeoutForDuration(30*time.Second, 90)

	if timeout <= 30*time.Second {
		t.Fatalf("timeout = %v, want more than legacy 30s default", timeout)
	}
	if timeout < 4*time.Minute {
		t.Fatalf("timeout = %v, want enough headroom for long local STT captures", timeout)
	}
}

func TestTranscriptionWorkerSubmitWhileClosingDoesNotPanic(t *testing.T) {
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 8,
		Runner: NewTranscriptionRunner(stubTranscriber{
			transcript: speechkit.Transcript{Text: "ok", Provider: "local", Duration: 10 * time.Millisecond},
		}, nil),
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	var panicCount atomic.Int64
	var wg sync.WaitGroup
	job := speechkit.TranscriptionJob{
		Submission: speechkit.Submission{
			PCM:          []byte(strings.Repeat("a", 6400)),
			WAV:          []byte("wav"),
			DurationSecs: 0.2,
			Language:     "en",
		},
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				func() {
					defer func() {
						if recover() != nil {
							panicCount.Add(1)
						}
					}()
					_ = worker.Submit(job)
				}()
			}
		}()
	}

	time.Sleep(20 * time.Millisecond)
	worker.Close()
	wg.Wait()
	worker.Wait()

	if panicCount.Load() != 0 {
		t.Fatalf("Submit panicked %d time(s)", panicCount.Load())
	}
}

func TestTranscriptionWorkerSuccessLogRedactsTranscriptText(t *testing.T) {
	observer := &recordingObserver{}
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 1,
		Runner: NewTranscriptionRunner(stubTranscriber{
			transcript: speechkit.Transcript{
				Text:     "secret customer text",
				Provider: "local",
				Duration: 1500 * time.Millisecond,
			},
		}, nil),
		Observer: observer,
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
			Language:     "en",
		},
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	worker.Close()
	worker.Wait()

	joined := strings.Join(observer.logs, "\n")
	if strings.Contains(joined, "secret customer text") {
		t.Fatalf("expected redacted success log, got logs: %s", joined)
	}
	if !strings.Contains(joined, "transcript ready") {
		t.Fatalf("expected success log marker, got logs: %s", joined)
	}
}

// panicOutput panics on delivery, simulating a host output-injection callback
// (e.g. WebView2/text-field paste) that faults during a transcript hand-off.
type panicOutput struct{}

func (panicOutput) Deliver(_ context.Context, _ speechkit.Transcript, _ any) error {
	panic("boom in output delivery")
}

// panicTranscriber panics on every call, simulating a provider adapter that
// faults while transcribing a segment.
type panicTranscriber struct{}

func (panicTranscriber) Transcribe(_ context.Context, _ []byte, _ float64, _ string) (speechkit.Transcript, error) {
	panic("boom in transcriber")
}

// A panic in synchronous delivery runs on the worker's job-loop goroutine.
// Without handleJobSafely's per-job recover it would crash the whole process
// (the desktop host installs no top-level panic handler). The worker must
// instead log the panic and keep processing the next job.
func TestTranscriptionWorkerRecoversFromOutputPanic(t *testing.T) {
	transcriber := &countingTranscriber{transcript: speechkit.Transcript{Text: "hallo", Provider: "deepgram"}}
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 4,
		Runner:    NewTranscriptionRunner(transcriber, nil),
		Output:    panicOutput{},
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	for i := 0; i < 2; i++ {
		if err := worker.Submit(speechkit.TranscriptionJob{
			Submission: speechkit.Submission{WAV: []byte("wav"), DurationSecs: 0.2, Language: "de"},
			Target:     "editor",
		}); err != nil {
			t.Fatalf("Submit() job %d error = %v", i, err)
		}
	}

	worker.Close()
	worker.Wait() // must return; a leaked panic would have aborted the test binary

	if got := transcriber.count(); got != 2 {
		t.Fatalf("transcriber calls = %d, want 2 (worker must keep running after a delivery panic)", got)
	}
}

// A multi-segment job runs each segment on its own goroutine, so a panic there
// is invisible to handleJobSafely's recover and needs the segment-level recover
// added in transcribeSegmentsParallel. Without it, this crashes the process.
func TestTranscriptionWorkerRecoversFromSegmentPanic(t *testing.T) {
	output := &recordingOutput{}
	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 1,
		Runner:    NewTranscriptionRunner(panicTranscriber{}, nil),
		Output:    output,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(speechkit.TranscriptionJob{
		Submission: speechkit.Submission{PCM: []byte(strings.Repeat("x", 6400)), WAV: []byte("full"), DurationSecs: 0.4, Language: "de"},
		Segments: []speechkit.Submission{
			{WAV: []byte("segment-1"), DurationSecs: 0.2, Language: "de"},
			{WAV: []byte("segment-2"), DurationSecs: 0.2, Language: "de"},
		},
		Target: "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	worker.Close()
	worker.Wait() // must return despite both segment goroutines panicking

	if got := output.snapshot(); len(got) != 0 {
		t.Fatalf("expected no delivery after all segments panicked, got %#v", got)
	}
}
