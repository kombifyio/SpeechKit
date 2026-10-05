package pipeline

import (
	"context"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// CompleteDictationStreamRecording retains the full source on the session's
// existing row. A pending final can create that row later with this capture.
func (w *TranscriptionWorker) CompleteDictationStreamRecording(parent context.Context, sessionID uint64, recording speechkit.Submission, opts speechkit.DictationStreamSinkOptions) {
	if w == nil || w.runner == nil || sessionID == 0 || opts.QuickNote || opts.CaptureChannel != "" || opts.RecordingSessionID != 0 {
		return
	}
	session := w.live.get(sessionID)
	if session == nil {
		return
	}
	session.persistMu.Lock()
	defer session.persistMu.Unlock()
	if session.recordingComplete {
		return
	}
	session.recordingComplete = true
	duration := recording.DurationSecs
	if duration <= 0 {
		duration = speechkit.PCMDurationSecs(recording.PCM)
	}
	session.recordingDurationMs = int64(duration * 1000)
	session.recordingAudio = append([]byte(nil), persistableAudio(recording)...)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 15*time.Second)
	defer cancel()
	if err := w.persistLiveRecording(ctx, session); err != nil {
		w.onLog("Transcription recording could not be saved", "warn")
	}
}

// Caller holds persistMu, preventing duplicate attachments and duration updates
// from racing later text commits.
func (w *TranscriptionWorker) persistLiveRecording(ctx context.Context, session *liveSession) error {
	if !session.recordingComplete || session.recordingSaved || session.rowID == 0 || session.rowless {
		return nil
	}
	writer, ok := w.runner.store.(speechkit.TranscriptionSessionAudioStore)
	if !ok {
		session.recordingAudio = nil
		return nil
	}
	if err := writer.UpdateTranscriptionAudio(ctx, session.rowID, session.recordingDurationMs, session.recordingAudio); err != nil {
		return err
	}
	session.recordingSaved = true
	session.recordingAudio = nil
	return nil
}
