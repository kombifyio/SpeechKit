package speechkit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const DefaultProcessingMessage = "Recording stopped · Transcribing"

var (
	ErrMissingRunner      = errors.New("speechkit: transcription worker requires a runner")
	ErrMissingTranscriber = errors.New("speechkit: transcription runner requires a transcriber")
	ErrWorkerClosed       = errors.New("speechkit: transcription worker is closed")
	ErrWorkerQueueFull    = errors.New("speechkit: transcription worker queue is full")
)

// TranscriptOutput delivers a completed [Transcript] to the host application
// (e.g. clipboard injection or text-field paste).
type TranscriptOutput interface {
	Deliver(ctx context.Context, transcript Transcript, target any) error
}

// TranscriptInterceptor can handle a transcript before it reaches the normal
// output path. Return (true, nil) to signal that the transcript was consumed.
type TranscriptInterceptor interface {
	Intercept(ctx context.Context, transcript Transcript, target any) (bool, error)
}

// TranscriptionObserver receives real-time status and log updates from a
// [TranscriptionWorker] during processing.
type TranscriptionObserver interface {
	OnState(status, text string)
	OnLog(message, kind string)
	OnTranscriptCommitted(transcript Transcript, quickNote bool)
}

// TranscriptionJob pairs a [Submission] with its delivery target.
type TranscriptionJob struct {
	Submission
	Target any
}

func (j TranscriptionJob) Clone() TranscriptionJob {
	clone := j
	clone.Submission = j.Submission
	if j.PCM != nil {
		clone.PCM = append([]byte(nil), j.PCM...)
	}
	if j.WAV != nil {
		clone.WAV = append([]byte(nil), j.WAV...)
	}
	return clone
}

// TranscriptionWorkerConfig configures a [TranscriptionWorker].
// Runner is required; all other fields are optional.
type TranscriptionWorkerConfig struct {
	Timeout     time.Duration
	QueueSize   int
	Runner      *TranscriptionRunner
	Output      TranscriptOutput
	Interceptor TranscriptInterceptor
	Observer    TranscriptionObserver
}

// TranscriptionWorker processes [TranscriptionJob] values from an internal
// queue on a single goroutine. Start it with [TranscriptionWorker.Start] and
// submit work with [TranscriptionWorker.Submit].
type TranscriptionWorker struct {
	timeout     time.Duration
	runner      *TranscriptionRunner
	output      TranscriptOutput
	interceptor TranscriptInterceptor
	observer    TranscriptionObserver

	mu      sync.Mutex
	jobs    chan TranscriptionJob
	done    chan struct{}
	started bool
	closed  bool
}

func NewTranscriptionWorker(cfg TranscriptionWorkerConfig) (*TranscriptionWorker, error) {
	if cfg.Runner == nil {
		return nil, ErrMissingRunner
	}
	if cfg.Runner.transcriber == nil {
		return nil, ErrMissingTranscriber
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 4
	}

	return &TranscriptionWorker{
		timeout:     cfg.Timeout,
		runner:      cfg.Runner,
		output:      cfg.Output,
		interceptor: cfg.Interceptor,
		observer:    cfg.Observer,
		jobs:        make(chan TranscriptionJob, cfg.QueueSize),
		done:        make(chan struct{}),
	}, nil
}

func (w *TranscriptionWorker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return
	}
	w.started = true
	jobs := w.jobs
	done := w.done
	w.mu.Unlock()

	go func() {
		defer close(done)
		for {
			select {
			case job, ok := <-jobs:
				if !ok {
					return
				}
				w.handleJob(ctx, job)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (w *TranscriptionWorker) Submit(job TranscriptionJob) error {
	job = job.Clone()

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return ErrWorkerClosed
	}

	select {
	case w.jobs <- job:
		return nil
	default:
		return ErrWorkerQueueFull
	}
}

func (w *TranscriptionWorker) Close() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	close(w.jobs)
	w.mu.Unlock()
}

func (w *TranscriptionWorker) Wait() {
	<-w.done
}

func (w *TranscriptionWorker) handleJob(ctx context.Context, job TranscriptionJob) {
	w.onState("processing", DefaultProcessingMessage)
	w.onLog("Sending to STT...", "info")

	transcribeCtx, cancel := context.WithTimeout(ctx, w.timeout)
	transcript, err := w.runner.transcriber.Transcribe(transcribeCtx, job.WAV, job.DurationSecs, job.Language)
	cancel()
	if err != nil {
		w.onLog(fmt.Sprintf("STT error: %v", err), "error")
		w.onState("idle", "")
		return
	}

	if w.interceptor != nil {
		handled, interceptErr := w.interceptor.Intercept(ctx, transcript, job.Target)
		if interceptErr != nil {
			w.onLog(fmt.Sprintf("Quick command error: %v", interceptErr), "error")
			w.onState("idle", "")
			return
		}
		if handled {
			w.onLog("Quick command handled", "success")
			w.onState("done", "")
			return
		}
	}

	completion, err := w.runner.Commit(ctx, job.Submission, transcript)
	if err != nil {
		w.onLog(fmt.Sprintf("Commit error: %v", err), "error")
		w.onState("idle", "")
		return
	}

	ms := completion.Transcript.Duration.Milliseconds()
	trimmedText := strings.TrimSpace(completion.Transcript.Text)
	w.onLog(
		fmt.Sprintf(
			"[%s] %dms: transcript committed (%d chars, %d words)",
			completion.Transcript.Provider,
			ms,
			utf8.RuneCountInString(trimmedText),
			len(strings.Fields(trimmedText)),
		),
		"success",
	)
	w.onState("done", completion.Transcript.Text)
	w.onTranscriptCommitted(completion.Transcript, job.QuickNote)

	if completion.QuickNoteCommitted {
		if completion.QuickNoteCreated {
			w.onLog("Quick Note saved", "success")
		} else {
			w.onLog(fmt.Sprintf("Quick Note #%d updated", completion.QuickNoteID), "success")
		}
		return
	}

	if w.output == nil {
		return
	}
	if err := w.output.Deliver(ctx, completion.Transcript, job.Target); err != nil {
		w.onLog(fmt.Sprintf("Output error: %v", err), "error")
	}
}

func (w *TranscriptionWorker) onState(status, text string) {
	if w.observer != nil {
		w.observer.OnState(status, text)
	}
}

func (w *TranscriptionWorker) onLog(message, kind string) {
	if w.observer != nil {
		w.observer.OnLog(message, kind)
	}
}

func (w *TranscriptionWorker) onTranscriptCommitted(transcript Transcript, quickNote bool) {
	if w.observer != nil {
		w.observer.OnTranscriptCommitted(transcript, quickNote)
	}
}
