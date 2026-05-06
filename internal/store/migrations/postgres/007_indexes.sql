CREATE INDEX IF NOT EXISTS idx_user_dictionary_canonical_lookup
    ON user_dictionary_entries(lower(canonical), language, enabled);

