package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

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

// outputNotConfirmedMessage is the warn line for a delivery that returned an
// error. When the adapter names why it refused (speechkit.OutputBlockReason),
// the phrase is appended so the notice says which of "window closed", "not
// in front" or "overlay had focus" it was; the error itself is never logged,
// because adapter errors may carry text.
func outputNotConfirmedMessage(err error) string {
	const base = "Text available; output was not confirmed"
	if reason := speechkit.OutputBlockReasonOf(err); reason != "" {
		return base + ": " + reason
	}
	return base
}
