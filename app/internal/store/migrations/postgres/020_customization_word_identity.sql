DELETE FROM customization_words a
    USING customization_words b
WHERE a.ctid < b.ctid
  AND a.scope_id = b.scope_id
  AND a.language = b.language
  AND a.source = b.source
  AND lower(a.term) = lower(b.term);

CREATE UNIQUE INDEX IF NOT EXISTS idx_customization_words_identity
    ON customization_words(scope_id, language, source, lower(term));
