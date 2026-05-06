CREATE INDEX IF NOT EXISTS idx_transcriptions_created_at_id
    ON transcriptions(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_quick_notes_pinned
    ON quick_notes(pinned DESC, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_user_dictionary_canonical_lookup
    ON user_dictionary_entries(lower(canonical), language, enabled);

