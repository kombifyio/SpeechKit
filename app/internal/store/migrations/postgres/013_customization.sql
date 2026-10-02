CREATE TABLE IF NOT EXISTS customization_words (
    id          TEXT NOT NULL,
    scope_id    BIGINT NOT NULL DEFAULT 1 REFERENCES storage_scopes(id) ON DELETE CASCADE,
    term        TEXT NOT NULL,
    sounds_like_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    language    TEXT NOT NULL DEFAULT '',
    weight      DOUBLE PRECISION NOT NULL DEFAULT 0,
    tags_json   JSONB NOT NULL DEFAULT '[]'::jsonb,
    source      TEXT NOT NULL DEFAULT 'settings',
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    usage_count INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(scope_id, id)
);

CREATE INDEX IF NOT EXISTS idx_customization_words_language
    ON customization_words(scope_id, language, enabled, term);

CREATE TABLE IF NOT EXISTS customization_replacements (
    id          TEXT NOT NULL,
    scope_id    BIGINT NOT NULL DEFAULT 1 REFERENCES storage_scopes(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    match_type  TEXT NOT NULL DEFAULT 'phrase',
    match_pattern TEXT NOT NULL,
    match_case_sensitive BOOLEAN NOT NULL DEFAULT FALSE,
    match_word_boundary  BOOLEAN NOT NULL DEFAULT TRUE,
    output_text TEXT NOT NULL DEFAULT '',
    output_intent TEXT NOT NULL DEFAULT '',
    output_template TEXT NOT NULL DEFAULT '',
    output_payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    language    TEXT NOT NULL DEFAULT '',
    modes_json  JSONB NOT NULL DEFAULT '[]'::jsonb,
    stage       TEXT NOT NULL,
    priority    INTEGER NOT NULL DEFAULT 0,
    tags_json   JSONB NOT NULL DEFAULT '[]'::jsonb,
    source      TEXT NOT NULL DEFAULT 'settings',
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    usage_count INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(scope_id, id)
);

CREATE INDEX IF NOT EXISTS idx_customization_replacements_stage
    ON customization_replacements(scope_id, language, stage, enabled, priority);

CREATE TABLE IF NOT EXISTS customization_lexicons (
    id          TEXT NOT NULL,
    scope_id    BIGINT NOT NULL DEFAULT 1 REFERENCES storage_scopes(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    language    TEXT NOT NULL DEFAULT '',
    word_ids_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    tags_json   JSONB NOT NULL DEFAULT '[]'::jsonb,
    source      TEXT NOT NULL DEFAULT 'settings',
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(scope_id, id)
);

CREATE TABLE IF NOT EXISTS customization_rulesets (
    id          TEXT NOT NULL,
    scope_id    BIGINT NOT NULL DEFAULT 1 REFERENCES storage_scopes(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    language    TEXT NOT NULL DEFAULT '',
    replacement_ids_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    tags_json   JSONB NOT NULL DEFAULT '[]'::jsonb,
    source      TEXT NOT NULL DEFAULT 'settings',
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(scope_id, id)
);

INSERT INTO customization_words
    (scope_id, id, term, language, source, enabled, usage_count, created_at, updated_at)
SELECT
    COALESCE(scope_id, 1),
    'legacy_dictionary_word_' || id::text,
    canonical,
    language,
    source,
    enabled,
    usage_count,
    created_at,
    updated_at
FROM user_dictionary_entries
WHERE TRIM(canonical) <> ''
ON CONFLICT DO NOTHING;

INSERT INTO customization_replacements
    (scope_id, id, kind, match_type, match_pattern, match_word_boundary, output_text, language, modes_json, stage, source, enabled, usage_count, created_at, updated_at)
SELECT
    COALESCE(scope_id, 1),
    'legacy_dictionary_replacement_' || id::text,
    'substitution',
    'spoken_alias',
    spoken,
    TRUE,
    canonical,
    language,
    '["dictation","assist"]'::jsonb,
    'post_stt',
    source,
    enabled,
    usage_count,
    created_at,
    updated_at
FROM user_dictionary_entries
WHERE TRIM(spoken) <> ''
  AND TRIM(canonical) <> ''
  AND lower(spoken) <> lower(canonical)
ON CONFLICT DO NOTHING;
