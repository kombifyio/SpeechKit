ALTER TABLE transcriptions
    ADD COLUMN IF NOT EXISTS pinned BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_transcriptions_pinned_created_at_id
    ON transcriptions(pinned DESC, created_at DESC, id DESC);
