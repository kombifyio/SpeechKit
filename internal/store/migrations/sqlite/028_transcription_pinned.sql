-- A transcription a user pins stays at the top of the Library and survives the
-- rolling history view. Dictation history is a stream people scroll back
-- through; the few lines worth keeping should not have to be found again.
CREATE INDEX IF NOT EXISTS idx_transcriptions_pinned_created_at_id
    ON transcriptions(pinned DESC, created_at DESC, id DESC);
