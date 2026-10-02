CREATE TABLE IF NOT EXISTS customization_words (
    id          TEXT NOT NULL,
    scope_id    INTEGER NOT NULL DEFAULT 1 REFERENCES storage_scopes(id) ON DELETE CASCADE,
    term        TEXT NOT NULL,
    sounds_like_json TEXT NOT NULL DEFAULT '[]',
    language    TEXT NOT NULL DEFAULT '',
    weight      REAL NOT NULL DEFAULT 0,
    tags_json   TEXT NOT NULL DEFAULT '[]',
    source      TEXT NOT NULL DEFAULT 'settings',
    enabled     INTEGER NOT NULL DEFAULT 1,
    usage_count INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(scope_id, id)
);

CREATE INDEX IF NOT EXISTS idx_customization_words_language
    ON customization_words(scope_id, language, enabled, term);

CREATE TABLE IF NOT EXISTS customization_replacements (
    id          TEXT NOT NULL,
    scope_id    INTEGER NOT NULL DEFAULT 1 REFERENCES storage_scopes(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    match_type  TEXT NOT NULL DEFAULT 'phrase',
    match_pattern TEXT NOT NULL,
    match_case_sensitive INTEGER NOT NULL DEFAULT 0,
    match_word_boundary  INTEGER NOT NULL DEFAULT 1,
    output_text TEXT NOT NULL DEFAULT '',
    output_intent TEXT NOT NULL DEFAULT '',
    output_template TEXT NOT NULL DEFAULT '',
    output_payload_json TEXT NOT NULL DEFAULT '{}',
    language    TEXT NOT NULL DEFAULT '',
    modes_json  TEXT NOT NULL DEFAULT '[]',
    stage       TEXT NOT NULL,
    priority    INTEGER NOT NULL DEFAULT 0,
    tags_json   TEXT NOT NULL DEFAULT '[]',
    source      TEXT NOT NULL DEFAULT 'settings',
    enabled     INTEGER NOT NULL DEFAULT 1,
    usage_count INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(scope_id, id)
);

CREATE INDEX IF NOT EXISTS idx_customization_replacements_stage
    ON customization_replacements(scope_id, language, stage, enabled, priority);

CREATE TABLE IF NOT EXISTS customization_lexicons (
    id          TEXT NOT NULL,
    scope_id    INTEGER NOT NULL DEFAULT 1 REFERENCES storage_scopes(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    language    TEXT NOT NULL DEFAULT '',
    word_ids_json TEXT NOT NULL DEFAULT '[]',
    tags_json   TEXT NOT NULL DEFAULT '[]',
    source      TEXT NOT NULL DEFAULT 'settings',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(scope_id, id)
);

CREATE TABLE IF NOT EXISTS customization_rulesets (
    id          TEXT NOT NULL,
    scope_id    INTEGER NOT NULL DEFAULT 1 REFERENCES storage_scopes(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    language    TEXT NOT NULL DEFAULT '',
    replacement_ids_json TEXT NOT NULL DEFAULT '[]',
    tags_json   TEXT NOT NULL DEFAULT '[]',
    source      TEXT NOT NULL DEFAULT 'settings',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(scope_id, id)
);

INSERT OR IGNORE INTO customization_words
    (scope_id, id, term, language, source, enabled, usage_count, created_at, updated_at)
SELECT
    COALESCE(scope_id, 1),
    'legacy_dictionary_word_' || id,
    canonical,
    language,
    source,
    enabled,
    usage_count,
    created_at,
    updated_at
FROM user_dictionary_entries
WHERE TRIM(canonical) != '';

INSERT OR IGNORE INTO customization_replacements
    (scope_id, id, kind, match_type, match_pattern, match_word_boundary, output_text, language, modes_json, stage, source, enabled, usage_count, created_at, updated_at)
SELECT
    COALESCE(scope_id, 1),
    'legacy_dictionary_replacement_' || id,
    'substitution',
    'spoken_alias',
    spoken,
    1,
    canonical,
    language,
    '["dictation","assist"]',
    'post_stt',
    source,
    enabled,
    usage_count,
    created_at,
    updated_at
FROM user_dictionary_entries
WHERE TRIM(spoken) != ''
  AND TRIM(canonical) != ''
  AND lower(spoken) != lower(canonical);
