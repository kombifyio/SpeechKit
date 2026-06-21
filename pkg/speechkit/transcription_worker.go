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

// TranscriptTransformer can apply final post-STT changes after all audio
// segments have been transcribed and merged, but before command routing or
// user-visible output.
type TranscriptTransformer interface {
	Transform(ctx context.Context, transcript Transcript) (Transcript, error)
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
	Segments []Submission
	Target   any
}

func (j TranscriptionJob) Clone() TranscriptionJob {
	clone := j
	clone.Submission = cloneSubmission(j.Submission)
	if j.Segments != nil {
		clone.Segments = make([]Submission, 0, len(j.Segments))
		for _, segment := range j.Segments {
			clone.Segments = append(clone.Segments, cloneSubmission(segment))
		}
	}
	return clone
}

func cloneSubmission(submission Submission) Submission {
	clone := submission
	if submission.PCM != nil {
		clone.PCM = append([]byte(nil), submission.PCM...)
	}
	if submission.WAV != nil {
		clone.WAV = append([]byte(nil), submission.WAV...)
	}
	return clone
}

func (j TranscriptionJob) transcriptionSegments() []Submission {
	if len(j.Segments) > 0 {
		return j.Segments
	}
	return []Submission{j.Submission}
}

// TranscriptionWorkerConfig configures a [TranscriptionWorker].
// Runner is required; all other fields are optional.
type TranscriptionWorkerConfig struct {
	Timeout     time.Duration
	QueueSize   int
	Runner      *TranscriptionRunner
	Output      TranscriptOutput
	Interceptor TranscriptInterceptor
	Transformer TranscriptTransformer
	Observer    TranscriptionObserver
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
	output      TranscriptOutput
	interceptor TranscriptInterceptor
	transformer TranscriptTransformer
	observer    TranscriptionObserver

	lowConfidenceThreshold float64

	mu        sync.Mutex
	persistWG sync.WaitGroup
	jobs      chan TranscriptionJob
	done      chan struct{}
	started   bool
	closed    bool
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
		timeout:                cfg.Timeout,
		runner:                 cfg.Runner,
		output:                 cfg.Output,
		interceptor:            cfg.Interceptor,
		transformer:            cfg.Transformer,
		observer:               cfg.Observer,
		lowConfidenceThreshold: cfg.LowConfidenceThreshold,
		jobs:                   make(chan TranscriptionJob, cfg.QueueSize),
		done:                   make(chan struct{}),
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
	w.persistWG.Wait()
}

func (w *TranscriptionWorker) handleJob(ctx context.Context, job TranscriptionJob) {
	w.onState("processing", DefaultProcessingMessage)

	segments := job.transcriptionSegments()
	if len(segments) > 1 {
		w.onLog(fmt.Sprintf("Sending %d segments to STT in parallel...", len(segments)), "info")
	} else {
		w.onLog("Sending to STT...", "info")
	}

	transcript, err := w.transcribeJob(ctx, job, segments)
	if err != nil {
		w.onLog(fmt.Sprintf("STT error: %v", err), "error")
		w.onState("idle", "")
		return
	}

	// Surface likely-misrecognized words so a silently-wrong transcript is at
	// least visible in the log/status feed. Offsets are not reliable after
	// downstream rewriting, so we report the raw low-confidence terms by text.
	if terms, minConf := LowConfidenceWords(transcript.Words, w.lowConfidenceThreshold); len(terms) > 0 {
		w.onLog(
			fmt.Sprintf("Low-confidence words (min %.2f, threshold %.2f): %s — add recurring terms to Vocabulary to fix",
				minConf, w.lowConfidenceThreshold, strings.Join(terms, ", ")),
			"warn",
		)
	}

	if w.transformer != nil {
		transformed, transformErr := w.transformer.Transform(ctx, transcript)
		if transformErr != nil {
			w.onLog(fmt.Sprintf("Transcript transform error: %v", transformErr), "error")
			w.onState("idle", "")
			return
		}
		transcript = transformed
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

	if job.QuickNote {
		completion, err := w.runner.Commit(ctx, job.Submission, transcript)
		if err != nil {
			w.onLog(fmt.Sprintf("Commit error: %v", err), "error")
			w.onState("idle", "")
			return
		}

		w.logTranscriptReady(completion.Transcript, "transcript committed")
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

		transcript = completion.Transcript
	} else {
		transcript.Text = normalizeTranscriptText(transcript.Text, job.Prefix)
		w.logTranscriptReady(transcript, "transcript ready")
		w.onState("done", transcript.Text)
		w.onTranscriptCommitted(transcript, false)
	}

	if w.output == nil {
		if !job.QuickNote {
			w.persistTranscriptionAsync(job.Submission, transcript) //nolint:contextcheck // fire-and-forget history write; uses its own 15s timeout context
		}
		return
	}
	if err := w.output.Deliver(ctx, transcript, job.Target); err != nil {
		w.onLog(fmt.Sprintf("Output error: %v", err), "error")
	}
	if !job.QuickNote {
		w.persistTranscriptionAsync(job.Submission, transcript) //nolint:contextcheck // fire-and-forget history write; uses its own 15s timeout context
	}
}

func (w *TranscriptionWorker) transcribeJob(ctx context.Context, job TranscriptionJob, segments []Submission) (Transcript, error) {
	if len(segments) == 0 {
		return Transcript{}, fmt.Errorf("empty transcription job")
	}

	durationSecs := job.DurationSecs
	if durationSecs <= 0 {
		for _, segment := range segments {
			durationSecs += segment.DurationSecs
		}
	}

	started := time.Now()
	transcribeCtx, cancel := context.WithTimeout(ctx, transcriptionTimeoutForDuration(w.timeout, durationSecs))
	defer cancel()

	if len(segments) == 1 {
		segment := inheritSegmentDefaults(job.Submission, segments[0])
		transcript, err := w.runner.transcriber.Transcribe(transcribeCtx, segment.WAV, segment.DurationSecs, segment.Language)
		if err != nil {
			return Transcript{}, err
		}
		if len(job.Segments) > 0 {
			return combineSegmentTranscripts(job.Submission, []Submission{segment}, []Transcript{transcript}, time.Since(started)), nil
		}
		return transcript, nil
	}

	return w.transcribeSegmentsParallel(transcribeCtx, cancel, job, segments, started)
}

func (w *TranscriptionWorker) transcribeSegmentsParallel(ctx context.Context, cancel context.CancelFunc, job TranscriptionJob, segments []Submission, started time.Time) (Transcript, error) {
	transcripts := make([]Transcript, len(segments))
	errs := make([]error, len(segments))

	var wg sync.WaitGroup
	for i, segment := range segments {
		i, segment := i, inheritSegmentDefaults(job.Submission, segment)
		wg.Add(1)
		go func() {
			defer wg.Done()
			transcript, err := w.runner.transcriber.Transcribe(ctx, segment.WAV, segment.DurationSecs, segment.Language)
			if err != nil {
				errs[i] = err
				cancel()
				return
			}
			transcripts[i] = transcript
			segments[i] = segment
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return Transcript{}, fmt.Errorf("segment %d: %w", i+1, err)
		}
	}

	return combineSegmentTranscripts(job.Submission, segments, transcripts, time.Since(started)), nil
}

func inheritSegmentDefaults(parent, segment Submission) Submission {
	if segment.Language == "" {
		segment.Language = parent.Language
	}
	if segment.DurationSecs <= 0 && len(segment.PCM) > 0 {
		segment.DurationSecs = PCMDurationSecs(segment.PCM)
	}
	if len(segment.WAV) == 0 && len(segment.PCM) > 0 {
		segment.WAV = PCMToWAV(segment.PCM)
	}
	return segment
}

func combineSegmentTranscripts(parent Submission, segments []Submission, transcripts []Transcript, elapsed time.Duration) Transcript {
	combined := Transcript{
		Language: parent.Language,
		Duration: elapsed,
	}
	var text strings.Builder
	var confidenceSum float64
	var confidenceCount int

	for i, transcript := range transcripts {
		if combined.Provider == "" {
			combined.Provider = transcript.Provider
		}
		if combined.Model == "" {
			combined.Model = transcript.Model
		}
		if combined.Language == "" {
			combined.Language = transcript.Language
		}
		if transcript.Confidence > 0 {
			confidenceSum += transcript.Confidence
			confidenceCount++
		}
		prefix := ""
		if i < len(segments) {
			prefix = segments[i].Prefix
		}
		appendTranscriptPart(&text, normalizeTranscriptText(transcript.Text, prefix))
		combined.Words = append(combined.Words, transcript.Words...)
	}

	combined.Text = text.String()
	if confidenceCount > 0 {
		combined.Confidence = confidenceSum / float64(confidenceCount)
	}
	return combined
}

func appendTranscriptPart(builder *strings.Builder, part string) {
	if builder == nil {
		return
	}
	part = strings.TrimRight(part, " \t")
	if strings.TrimSpace(part) == "" {
		return
	}
	if builder.Len() == 0 || strings.HasPrefix(part, "\n") {
		builder.WriteString(part)
		return
	}
	builder.WriteByte(' ')
	builder.WriteString(strings.TrimLeft(part, " \t"))
}

func (w *TranscriptionWorker) logTranscriptReady(transcript Transcript, marker string) {
	ms := transcript.Duration.Milliseconds()
	trimmedText := strings.TrimSpace(transcript.Text)
	w.onLog(
		fmt.Sprintf(
			"[%s] %dms: %s (%d chars, %d words)",
			transcript.Provider,
			ms,
			marker,
			utf8.RuneCountInString(trimmedText),
			len(strings.Fields(trimmedText)),
		),
		"success",
	)
}

func (w *TranscriptionWorker) persistTranscriptionAsync(submission Submission, transcript Transcript) {
	if w.runner == nil {
		return
	}
	if w.runner.store == nil {
		w.runner.notifyCommit(Completion{Transcript: transcript})
		return
	}

	w.persistWG.Add(1)
	go func() {
		defer w.persistWG.Done()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		durationMs := int64(submission.DurationSecs * 1000)
		latencyMs := transcript.Duration.Milliseconds()
		if err := w.runner.store.SaveTranscription(ctx, transcript.Text, transcript.Language, transcript.Provider, transcript.Model, durationMs, latencyMs, submission.WAV); err != nil {
			w.onLog(fmt.Sprintf("Transcription history error: %v", err), "warn")
			return
		}

		w.runner.notifyCommit(Completion{
			Transcript:             transcript,
			TranscriptionPersisted: true,
		})
	}()
}

func transcriptionTimeoutForDuration(base time.Duration, durationSecs float64) time.Duration {
	if base <= 0 {
		base = 30 * time.Second
	}
	timeout := base
	if durationSecs > 0 {
		scaled := 20*time.Second + time.Duration(durationSecs*3*float64(time.Second))
		if scaled > timeout {
			timeout = scaled
		}
	}
	if timeout < 60*time.Second {
		timeout = 60 * time.Second
	}
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}
	return timeout
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
