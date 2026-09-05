package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

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

// EndRecordingSession releases deduplication history for a durable recording
// after its capture lifecycle has ended.
func (w *TranscriptionWorker) EndRecordingSession(recordingSessionID int64) {
	if w == nil {
		return
	}
	w.ledger.EndRecordingSession(recordingSessionID)
}

func (w *TranscriptionWorker) handleJob(ctx context.Context, job speechkit.TranscriptionJob) {
	w.onState("processing", speechkit.DefaultProcessingMessage)

	job.Submission = inheritSegmentDefaults(speechkit.Submission{}, job.Submission)
	segmentKey := transcriptSegmentKey(job.Submission)
	if !w.ledger.Begin(segmentKey) {
		w.onLog("Duplicate transcript segment skipped", "warn")
		w.onState("idle", "")
		return
	}
	segmentCommitted := false
	defer func() {
		if segmentCommitted {
			w.ledger.Commit(segmentKey)
			return
		}
		w.ledger.Release(segmentKey)
	}()

	segments := job.EffectiveSegments()
	if queuedAt := earliestQueuedAt(job, segments); !queuedAt.IsZero() {
		w.onLog(
			fmt.Sprintf("STT timing: queue_wait=%dms segments=%d audio=%.1fs",
				time.Since(queuedAt).Milliseconds(),
				len(segments),
				totalSubmissionDuration(job, segments),
			),
			"info",
		)
	}
	if len(segments) > 1 {
		w.onLog(fmt.Sprintf("Sending %d segments to STT in parallel...", len(segments)), "info")
	} else {
		w.onLog("Sending to STT...", "info")
	}

	transcript, err := w.transcribeJob(ctx, job, segments)
	if err != nil {
		finalization := speechkit.NewTranscriptionFinalization(transcript, err, false, false)
		w.onFinalization(job, transcript, finalization)
		w.onLog("Transcription failed", "error")
		w.onState("idle", "Transcription failed")
		return
	}
	applyTranscriptSessionMetadata(&transcript, job.Submission)

	segmentCommitted = w.commitFinalTranscript(ctx, job, transcript)
}

// HandleDictationStreamEvent routes provider-native dictation events through
// the same final-commit path as batch transcription. Interim/draft events are
// UI/status-only and must never call output or persistence.
func (w *TranscriptionWorker) HandleDictationStreamEvent(ctx context.Context, event speechkit.DictationStreamEvent, opts speechkit.DictationStreamSinkOptions) error {
	if w == nil {
		return ErrMissingRunner
	}
	transcript := event.Transcript()
	if transcript.Language == "" {
		transcript.Language = opts.Language
	}
	if !event.IsFinal {
		w.onTranscriptDraft(transcript)
		w.onState("transcribing", transcript.Text)
		return nil
	}
	// Provider-native streams deliver finals as they are recognized, so the
	// receipt time is the closest wall clock this path has to when the words
	// were spoken. Word-level timings can sharpen this later.
	finalizedAt := time.Now()
	capturedMs := elapsedCaptureMs(opts.CaptureEpoch, finalizedAt)
	submission := speechkit.Submission{
		Language:           opts.Language,
		QuickNote:          opts.QuickNote,
		QuickNoteID:        opts.QuickNoteID,
		SessionID:          transcript.SessionID,
		SegmentID:          transcript.SegmentID,
		ProviderItemID:     transcript.ProviderItemID,
		SegmentFinal:       true,
		RecordingSessionID: opts.RecordingSessionID,
		CaptureChannel:     opts.CaptureChannel,
		CapturedStartMs:    capturedMs,
		CapturedEndMs:      capturedMs,
		QueuedAt:           finalizedAt,
	}
	transcript.RecordingSessionID = opts.RecordingSessionID
	transcript.CaptureChannel = opts.CaptureChannel
	transcript.CapturedStartMs = capturedMs
	transcript.CapturedEndMs = capturedMs
	job := speechkit.TranscriptionJob{
		Submission: submission,
		Target:     opts.Target,
	}
	w.onState("processing", speechkit.DefaultProcessingMessage)
	segmentKey := transcriptSegmentKey(job.Submission)
	if !w.ledger.Begin(segmentKey) {
		w.onLog("Duplicate transcript segment skipped", "warn")
		w.onState("idle", "")
		return nil
	}
	segmentCommitted := false
	defer func() {
		if segmentCommitted {
			w.ledger.Commit(segmentKey)
			return
		}
		w.ledger.Release(segmentKey)
	}()
	segmentCommitted = w.commitFinalTranscript(ctx, job, transcript)
	return nil
}

func (w *TranscriptionWorker) prefixLiveInject(sessionID uint64, text string) string {
	w.liveInjectMu.Lock()
	defer w.liveInjectMu.Unlock()
	fragment, tail, session := LiveInjectFragment(w.liveInjectSession, w.liveInjectTail, text, sessionID)
	w.liveInjectSession = session
	w.liveInjectTail = tail
	return fragment
}

func (w *TranscriptionWorker) commitFinalTranscript(ctx context.Context, job speechkit.TranscriptionJob, transcript speechkit.Transcript) bool {
	// An empty final transcript is an outcome, not a non-event. Both the batch
	// and the streaming path funnel through here, so this is the one place that
	// has to name it; below this line every branch assumes there is text to
	// transform, intercept, commit or deliver.
	if strings.TrimSpace(transcript.Text) == "" {
		return w.commitEmptyFinalTranscript(ctx, job, transcript)
	}

	// Surface confidence without writing recognized words to the log.
	if terms, minConf := speechkit.LowConfidenceWords(transcript.Words, w.lowConfidenceThreshold); len(terms) > 0 {
		w.onLog(
			fmt.Sprintf("Low-confidence recognition (min %.2f, threshold %.2f) — review the transcript",
				minConf, w.lowConfidenceThreshold),
			"warn",
		)
	}

	if w.transformer != nil {
		transformed, transformErr := w.transformer.Transform(ctx, transcript)
		if transformErr != nil {
			return w.finishWithoutOutput(ctx, job, transcript, transformErr)
		}
		transcript = transformed
		applyTranscriptSessionMetadata(&transcript, job.Submission)
	}
	if strings.TrimSpace(transcript.Text) == "" {
		return w.commitEmptyFinalTranscript(ctx, job, transcript)
	}

	if w.interceptor != nil {
		handled, interceptErr := w.interceptor.Intercept(ctx, transcript, job.Target)
		if interceptErr != nil {
			return w.finishWithoutOutput(ctx, job, transcript, interceptErr)
		}
		if handled {
			w.onLog("Quick command handled", "success")
			w.onState("done", "")
			return true
		}
	}

	if job.QuickNote {
		completion, err := w.runner.Commit(ctx, job.Submission, transcript)
		if err != nil {
			w.onLog(fmt.Sprintf("Commit error: %v", err), "error")
			w.onState("idle", "")
			return false
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
			return true
		}

		transcript = completion.Transcript
	} else {
		transcript.Text = normalizeTranscriptText(transcript.Text, job.Prefix)
		w.logTranscriptReady(transcript, "transcript ready")
	}

	finalization := speechkit.NewTranscriptionFinalization(transcript, nil,
		w.output != nil && deliverableAsOutput(job.Submission), w.runner.store != nil)
	w.onFinalization(job, transcript, finalization)
	if finalization.Output == speechkit.OutputRequested {
		deliverStarted := time.Now()
		inject := transcript
		inject.Text = w.prefixLiveInject(transcript.SessionID, transcript.Text)
		err := w.output.Deliver(ctx, inject, job.Target)
		finalization = finalization.WithOutputResult(err)
		w.onFinalization(job, transcript, finalization)
		if err != nil {
			w.onLog("Text available; output was not confirmed", "warn")
		}
		w.onLog(fmt.Sprintf("STT timing: output_delivery=%dms", time.Since(deliverStarted).Milliseconds()), "info")
	}
	if !job.QuickNote {
		w.onTranscriptCommitted(transcript, false)
		w.onState(finalizationState(finalization, transcript.Text))
		w.persistTranscriptionAsync(ctx, job, transcript, finalization)
	}
	return true
}

// Keep recognized text recoverable even when post-recognition processing fails.
func (w *TranscriptionWorker) finishWithoutOutput(ctx context.Context, job speechkit.TranscriptionJob, transcript speechkit.Transcript, err error) bool {
	if job.QuickNote || job.CaptureChannel != "" {
		w.onLog("Transcript processing failed", "error")
		w.onState("idle", "")
		return false
	}
	f := speechkit.NewTranscriptionFinalization(transcript, nil, false, w.runner.store != nil)
	f = f.WithOutputResult(err)
	w.onFinalization(job, transcript, f)
	w.onLog("Text available; final processing failed", "warn")
	w.onState(finalizationState(f, transcript.Text))
	w.onTranscriptCommitted(transcript, false)
	w.persistTranscriptionAsync(ctx, job, transcript, f)
	return true
}

func finalizationState(f speechkit.TranscriptionFinalization, text string) (string, string) {
	switch f.Output {
	case speechkit.OutputBlocked:
		return "idle", "Text available, not inserted"
	case speechkit.OutputFailed:
		return "idle", "Text available; insertion unconfirmed"
	default:
		return "done", text
	}
}

// commitEmptyFinalTranscript records a successful transcription that produced
// no text: it logs, sets a terminal state the user can see, and persists the
// attempt so the loss is visible in history rather than only in the moment.
//
// It deliberately does not run the transformer, the quick-command interceptor,
// the commit runner or output delivery. There is nothing to rewrite, no command
// to match, no note worth saving, and delivering an empty string would paste
// nothing over whatever the user had selected.
//
// Returns true because the segment is finished, not because it succeeded — the
// same audio would produce the same empty result, so releasing it for a retry
// would only loop.
func (w *TranscriptionWorker) commitEmptyFinalTranscript(ctx context.Context, job speechkit.TranscriptionJob, transcript speechkit.Transcript) bool {
	finalization := speechkit.NewTranscriptionFinalization(transcript, nil, false, w.runner.store != nil)
	w.onFinalization(job, transcript, finalization)
	provider := firstNonEmptyField(transcript.Provider, "unknown")
	model := firstNonEmptyField(transcript.Model, "unknown")
	language := firstNonEmptyField(transcript.Language, "unset")
	speechkit.RecordOutcome(ctx, speechkit.OutcomeEmptyFinalTranscript, errors.New(speechkit.EmptyFinalTranscriptMessage),
		speechkit.StringAttr("provider", provider),
		speechkit.StringAttr("model", model),
		speechkit.StringAttr("language", language),
	)
	w.onLog(
		fmt.Sprintf("%s (provider=%s model=%s language=%s)",
			speechkit.EmptyFinalTranscriptMessage,
			provider,
			model,
			language,
		),
		"warn",
	)
	if job.QuickNote || job.CaptureChannel != "" {
		w.onState("done", speechkit.EmptyFinalTranscriptMessage)
	} else {
		w.onState("idle", speechkit.EmptyFinalTranscriptMessage)
	}
	if !job.QuickNote {
		w.persistTranscriptionAsync(ctx, job, transcript, finalization)
	}
	return true
}

func firstNonEmptyField(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstNonNilLedger(ledger *TranscriptSessionLedger) *TranscriptSessionLedger {
	if ledger != nil {
		return ledger
	}
	return NewTranscriptSessionLedger()
}

func (w *TranscriptionWorker) transcribeJob(ctx context.Context, job speechkit.TranscriptionJob, segments []speechkit.Submission) (speechkit.Transcript, error) {
	if len(segments) == 0 {
		return speechkit.Transcript{}, fmt.Errorf("empty transcription job")
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
		normalizeStarted := time.Now()
		segment := inheritSegmentDefaults(job.Submission, segments[0])
		normalizeElapsed := time.Since(normalizeStarted)
		providerStarted := time.Now()
		transcript, err := w.runner.transcriber.Transcribe(transcribeCtx, segment.WAV, segment.DurationSecs, segment.Language)
		providerElapsed := time.Since(providerStarted)
		if err != nil {
			w.onLog(
				fmt.Sprintf("STT timing: normalize=%dms provider_roundtrip=%dms audio=%.1fs provider=unknown model=unknown language=%s status=error",
					normalizeElapsed.Milliseconds(),
					providerElapsed.Milliseconds(),
					segment.DurationSecs,
					timingLogValue(segment.Language),
				),
				"info",
			)
			return speechkit.Transcript{}, err
		}
		w.onLog(
			fmt.Sprintf("STT timing: normalize=%dms provider_roundtrip=%dms audio=%.1fs provider=%s model=%s language=%s status=ok",
				normalizeElapsed.Milliseconds(),
				providerElapsed.Milliseconds(),
				segment.DurationSecs,
				timingLogValue(transcript.Provider),
				timingLogValue(transcript.Model),
				timingLogValue(transcript.Language),
			),
			"info",
		)
		if len(job.Segments) > 0 {
			return combineSegmentTranscripts(job.Submission, []speechkit.Submission{segment}, []speechkit.Transcript{transcript}, time.Since(started)), nil
		}
		return transcript, nil
	}

	return w.transcribeSegmentsParallel(transcribeCtx, cancel, job, segments, started)
}

func (w *TranscriptionWorker) transcribeSegmentsParallel(ctx context.Context, cancel context.CancelFunc, job speechkit.TranscriptionJob, segments []speechkit.Submission, started time.Time) (speechkit.Transcript, error) {
	transcripts := make([]speechkit.Transcript, len(segments))
	errs := make([]error, len(segments))

	var wg sync.WaitGroup
	for i, segment := range segments {
		i, segment := i, segment
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("speechkit: transcription worker segment panic recovered",
						"segment", i+1,
						"err", r,
						"stack", string(debug.Stack()),
					)
					errs[i] = fmt.Errorf("speechkit: transcription segment %d panicked: %v", i+1, r)
					cancel()
				}
			}()
			normalizeStarted := time.Now()
			segment = inheritSegmentDefaults(job.Submission, segment)
			normalizeElapsed := time.Since(normalizeStarted)
			providerStarted := time.Now()
			transcript, err := w.runner.transcriber.Transcribe(ctx, segment.WAV, segment.DurationSecs, segment.Language)
			providerElapsed := time.Since(providerStarted)
			if err != nil {
				w.onLog(
					fmt.Sprintf("STT timing: segment=%d normalize=%dms provider_roundtrip=%dms audio=%.1fs provider=unknown model=unknown language=%s status=error",
						i+1,
						normalizeElapsed.Milliseconds(),
						providerElapsed.Milliseconds(),
						segment.DurationSecs,
						timingLogValue(segment.Language),
					),
					"info",
				)
				errs[i] = err
				cancel()
				return
			}
			w.onLog(
				fmt.Sprintf("STT timing: segment=%d normalize=%dms provider_roundtrip=%dms audio=%.1fs provider=%s model=%s language=%s status=ok",
					i+1,
					normalizeElapsed.Milliseconds(),
					providerElapsed.Milliseconds(),
					segment.DurationSecs,
					timingLogValue(transcript.Provider),
					timingLogValue(transcript.Model),
					timingLogValue(transcript.Language),
				),
				"info",
			)
			transcripts[i] = transcript
			segments[i] = segment
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return speechkit.Transcript{}, fmt.Errorf("segment %d: %w", i+1, err)
		}
	}

	return combineSegmentTranscripts(job.Submission, segments, transcripts, time.Since(started)), nil
}

func earliestQueuedAt(job speechkit.TranscriptionJob, segments []speechkit.Submission) time.Time {
	queuedAt := job.QueuedAt
	for _, segment := range segments {
		if segment.QueuedAt.IsZero() {
			continue
		}
		if queuedAt.IsZero() || segment.QueuedAt.Before(queuedAt) {
			queuedAt = segment.QueuedAt
		}
	}
	return queuedAt
}

func totalSubmissionDuration(job speechkit.TranscriptionJob, segments []speechkit.Submission) float64 {
	if job.DurationSecs > 0 {
		return job.DurationSecs
	}
	var total float64
	for _, segment := range segments {
		if segment.DurationSecs > 0 {
			total += segment.DurationSecs
			continue
		}
		total += speechkit.PCMDurationSecs(segment.PCM)
	}
	return total
}

func inheritSegmentDefaults(parent, segment speechkit.Submission) speechkit.Submission {
	if segment.Language == "" {
		segment.Language = parent.Language
	}
	if segment.RecordingSessionID == 0 {
		segment.RecordingSessionID = parent.RecordingSessionID
	}
	if segment.CaptureChannel == "" {
		segment.CaptureChannel = parent.CaptureChannel
	}
	if segment.CapturedStartMs == 0 && segment.CapturedEndMs == 0 {
		segment.CapturedStartMs = parent.CapturedStartMs
		segment.CapturedEndMs = parent.CapturedEndMs
	}
	if segment.DurationSecs <= 0 && len(segment.PCM) > 0 {
		segment.DurationSecs = speechkit.PCMDurationSecs(segment.PCM)
	}
	if len(segment.WAV) == 0 && len(segment.PCM) > 0 {
		segment.WAV = speechkit.PCMToWAV(segment.PCM)
	}
	return segment
}

func combineSegmentTranscripts(parent speechkit.Submission, segments []speechkit.Submission, transcripts []speechkit.Transcript, elapsed time.Duration) speechkit.Transcript {
	combined := speechkit.Transcript{
		Language:           parent.Language,
		Duration:           elapsed,
		RecordingSessionID: parent.RecordingSessionID,
		CaptureChannel:     parent.CaptureChannel,
		CapturedStartMs:    parent.CapturedStartMs,
		CapturedEndMs:      parent.CapturedEndMs,
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

func (w *TranscriptionWorker) logTranscriptReady(transcript speechkit.Transcript, marker string) {
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

func timingLogValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func (w *TranscriptionWorker) persistTranscriptionAsync(parent context.Context, job speechkit.TranscriptionJob, transcript speechkit.Transcript, finalization speechkit.TranscriptionFinalization) {
	if w.runner == nil {
		return
	}
	submission := job.Submission
	durationMs := int64(submission.DurationSecs * 1000)
	if w.runner.store == nil {
		w.runner.notifyCommit(speechkit.Completion{Transcript: transcript, AudioDurationMs: durationMs})
		return
	}

	w.persistWG.Add(1)
	go func() {
		defer w.persistWG.Done()
		defer w.recoverWorkerGoroutine("persistTranscription")

		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 15*time.Second)
		defer cancel()

		latencyMs := transcript.Duration.Milliseconds()
		if err := w.runner.store.SaveTranscription(ctx, transcript.Text, transcript.Language, transcript.Provider, transcript.Model, durationMs, latencyMs, persistableAudio(submission)); err != nil {
			w.onFinalization(job, transcript, finalization.WithPersistenceResult(err))
			w.onLog("Transcription history could not be saved", "warn")
			return
		}
		w.onFinalization(job, transcript, finalization.WithPersistenceResult(nil))

		w.runner.notifyCommit(speechkit.Completion{
			Transcript:             transcript,
			TranscriptionPersisted: true,
			AudioDurationMs:        durationMs,
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

func (w *TranscriptionWorker) onTranscriptCommitted(transcript speechkit.Transcript, quickNote bool) {
	if w.observer != nil {
		w.observer.OnTranscriptCommitted(transcript, quickNote)
	}
}

func (w *TranscriptionWorker) onFinalization(job speechkit.TranscriptionJob, transcript speechkit.Transcript, finalization speechkit.TranscriptionFinalization) {
	if job.QuickNote || job.CaptureChannel != "" {
		return
	}
	if observer, ok := w.observer.(speechkit.TranscriptionFinalizationObserver); ok {
		observer.OnTranscriptionFinalized(transcript, finalization, job.Target)
	}
}

func (w *TranscriptionWorker) onTranscriptDraft(transcript speechkit.Transcript) {
	if w.observer == nil {
		return
	}
	if observer, ok := w.observer.(speechkit.TranscriptionDraftObserver); ok {
		observer.OnTranscriptDraft(transcript)
	}
}
