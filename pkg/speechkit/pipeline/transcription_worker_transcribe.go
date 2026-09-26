package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

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
