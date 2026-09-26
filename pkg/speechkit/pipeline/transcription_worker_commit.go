package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

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
			w.onLog(outputNotConfirmedMessage(err), "warn")
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
