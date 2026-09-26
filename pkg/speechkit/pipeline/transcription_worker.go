package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// Errors of the transcription worker. [NewTranscriptionWorker] returns
// ErrMissingRunner without a Runner and ErrMissingTranscriber when the runner
// has no transcriber; [TranscriptionWorker.Submit] returns ErrWorkerClosed
// after Close and ErrWorkerQueueFull when no queue slot is free.
var (
	ErrMissingRunner      = errors.New("speechkit: transcription worker requires a runner")
	ErrMissingTranscriber = errors.New("speechkit: transcription runner requires a transcriber")
	ErrWorkerClosed       = errors.New("speechkit: transcription worker is closed")
	ErrWorkerQueueFull    = errors.New("speechkit: transcription worker queue is full")
)

// TranscriptionWorkerConfig configures a [TranscriptionWorker].
// Runner is required; all other fields are optional.
type TranscriptionWorkerConfig struct {
	Timeout     time.Duration
	QueueSize   int
	Runner      *TranscriptionRunner
	Output      speechkit.TranscriptOutput
	Interceptor speechkit.TranscriptInterceptor
	Transformer speechkit.TranscriptTransformer
	Observer    speechkit.TranscriptionObserver
	Ledger      *TranscriptSessionLedger
	// LowConfidenceThreshold flags recognized words below this acoustic
	// confidence (0..1) so the host can surface likely-misrecognized terms.
	// <= 0 disables the check. Only providers that expose per-word confidence
	// (Deepgram, AssemblyAI) produce data here.
	LowConfidenceThreshold float64
}

// TranscriptionWorker processes [TranscriptionJob] values from an internal
// queue on a single goroutine. Start it with [TranscriptionWorker.Start] and
// submit work with [TranscriptionWorker.Submit].
type TranscriptionWorker struct {
	timeout     time.Duration
	runner      *TranscriptionRunner
	output      speechkit.TranscriptOutput
	interceptor speechkit.TranscriptInterceptor
	transformer speechkit.TranscriptTransformer
	observer    speechkit.TranscriptionObserver
	ledger      *TranscriptSessionLedger

	lowConfidenceThreshold float64

	mu        sync.Mutex
	persistWG sync.WaitGroup
	jobs      chan speechkit.TranscriptionJob
	done      chan struct{}
	started   bool
	closed    bool

	liveInjectMu      sync.Mutex
	liveInjectSession uint64
	liveInjectTail    string
}

// NewTranscriptionWorker validates cfg and builds a worker that is not yet
// running; call [TranscriptionWorker.Start]. It returns [ErrMissingRunner] or
// [ErrMissingTranscriber] for an unusable runner. Defaults: Timeout 30s (the
// base of the per-segment transcription deadline, which grows with audio
// length), QueueSize 4, and a private [TranscriptSessionLedger] when none is
// shared.
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
		timeout:                cfg.Timeout,
		runner:                 cfg.Runner,
		output:                 cfg.Output,
		interceptor:            cfg.Interceptor,
		transformer:            cfg.Transformer,
		observer:               cfg.Observer,
		ledger:                 firstNonNilLedger(cfg.Ledger),
		lowConfidenceThreshold: cfg.LowConfidenceThreshold,
		jobs:                   make(chan speechkit.TranscriptionJob, cfg.QueueSize),
		done:                   make(chan struct{}),
	}, nil
}

// Start launches the single goroutine that drains the queue. It returns
// immediately and is idempotent. The goroutine exits when ctx is done or the
// queue was closed by [TranscriptionWorker.Close] and drained; a panic in a
// job is recovered and logged.
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
				w.handleJobSafely(ctx, job)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// handleJobSafely runs handleJob under per-job panic recovery so a bug in
// transcription delivery, transformation, persistence, or a host/observer
// callback cannot crash the whole process. On panic the worker logs the stack
// (see recoverWorkerGoroutine) and keeps running to process the next job.
func (w *TranscriptionWorker) handleJobSafely(ctx context.Context, job speechkit.TranscriptionJob) {
	defer w.recoverWorkerGoroutine("handleJob")
	w.handleJob(ctx, job)
}

// recoverWorkerGoroutine recovers a panic raised on one of the transcription
// worker's goroutines (the job loop, the per-segment STT calls, and the async
// persistence write) and logs the stack instead of letting it tear down the
// app. These goroutines run outside any HTTP handler, so the server's Recover
// middleware cannot protect them, and the Windows desktop host installs no
// process-wide panic handler — an unrecovered panic here writes its stack only
// to stderr, which a GUI-subsystem binary discards, so the app just vanishes
// with no log. Use as: defer w.recoverWorkerGoroutine("name").
func (w *TranscriptionWorker) recoverWorkerGoroutine(name string) {
	if r := recover(); r != nil {
		slog.Error("speechkit: transcription worker goroutine panic recovered",
			"goroutine", name,
			"err", r,
			"stack", string(debug.Stack()),
		)
	}
}

// Submit implements [speechkit.JobSubmitter]. It stores a copy of job with
// QueuedAt stamped on the job and its segments and enqueues it without
// blocking. It returns [ErrWorkerClosed] after Close and [ErrWorkerQueueFull]
// when the queue is full; the caller decides whether to drop or retry.
func (w *TranscriptionWorker) Submit(job speechkit.TranscriptionJob) error {
	job = job.Clone()
	now := time.Now()
	if job.QueuedAt.IsZero() {
		job.QueuedAt = now
	}
	for i := range job.Segments {
		if job.Segments[i].QueuedAt.IsZero() {
			job.Segments[i].QueuedAt = job.QueuedAt
		}
	}

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

// Close stops accepting jobs and closes the queue so the worker goroutine
// exits once queued jobs are processed. It does not wait; see
// [TranscriptionWorker.Wait]. Idempotent.
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

// Wait blocks until the worker goroutine has exited (after Close or context
// cancellation) and every asynchronous history write has finished. It only
// returns for a worker that was started.
func (w *TranscriptionWorker) Wait() {
	<-w.done
	w.persistWG.Wait()
}

// EndRecordingSession releases deduplication history for a durable recording
// after its capture lifecycle has ended.
func (w *TranscriptionWorker) EndRecordingSession(recordingSessionID int64) {
	if w == nil {
		return
	}
	w.ledger.EndRecordingSession(recordingSessionID)
}

func firstNonNilLedger(ledger *TranscriptSessionLedger) *TranscriptSessionLedger {
	if ledger != nil {
		return ledger
	}
	return NewTranscriptSessionLedger()
}
