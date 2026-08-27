DELETE FROM customization_words
WHERE rowid NOT IN (
    SELECT MIN(rowid)
    FROM customization_words
    GROUP BY scope_id, language, source, lower(term)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_customization_words_identity
    ON customization_words(scope_id, language, source, lower(term));
