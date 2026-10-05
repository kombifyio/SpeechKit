package speechkit

import "context"

// DictationStreamRecordingSink optionally receives the original full capture
// once a stream stops. Hosts call it before ending the session; it does not
// transcribe or deliver text again. Persistence retains the store's audio policy.
type DictationStreamRecordingSink interface {
	CompleteDictationStreamRecording(ctx context.Context, sessionID uint64, recording Submission, opts DictationStreamSinkOptions)
}

// TranscriptionSessionAudioStore attaches a completed recording to the existing
// live history entry, retaining the backing store's scope and privacy policy.
type TranscriptionSessionAudioStore interface {
	UpdateTranscriptionAudio(ctx context.Context, id int64, durationMs int64, audioData []byte) error
}
