CREATE TABLE IF NOT EXISTS transcriptions (
    id            BIGSERIAL PRIMARY KEY,
    text          TEXT NOT NULL,
    language      TEXT NOT NULL DEFAULT '',
    language_base TEXT NOT NULL DEFAULT '',
    provider      TEXT NOT NULL,
    model         TEXT NOT NULL DEFAULT '',
    duration_ms   BIGINT NOT NULL DEFAULT 0,
    latency_ms    BIGINT NOT NULL DEFAULT 0,
    audio_path    TEXT NOT NULL DEFAULT '',
    word_count    BIGINT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE transcriptions
    ADD COLUMN IF NOT EXISTS language_base TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_transcriptions_created_at_id
    ON transcriptions(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_transcriptions_language_created_at_id
    ON transcriptions(language_base, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS quick_notes (
    id            BIGSERIAL PRIMARY KEY,
    text          TEXT NOT NULL,
    language      TEXT NOT NULL DEFAULT '',
    language_base TEXT NOT NULL DEFAULT '',
    provider      TEXT NOT NULL,
    duration_ms   BIGINT NOT NULL DEFAULT 0,
    latency_ms    BIGINT NOT NULL DEFAULT 0,
    audio_path    TEXT NOT NULL DEFAULT '',
    word_count    BIGINT NOT NULL DEFAULT 0,
    pinned        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE quick_notes
    ADD COLUMN IF NOT EXISTS language_base TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_quick_notes_created_at_id
    ON quick_notes(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_quick_notes_pinned_created_at_id
    ON quick_notes(pinned DESC, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_quick_notes_language_created_at_id
    ON quick_notes(language_base, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS audio_assets (
    id           BIGSERIAL PRIMARY KEY,
    owner_kind   TEXT NOT NULL DEFAULT '',
    owner_id     BIGINT NOT NULL DEFAULT 0,
    storage_kind TEXT NOT NULL DEFAULT 'local-file',
    path         TEXT NOT NULL,
    mime_type    TEXT NOT NULL DEFAULT 'audio/wav',
    size_bytes   BIGINT NOT NULL DEFAULT 0,
    duration_ms  BIGINT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(owner_kind, owner_id, path)
);

CREATE INDEX IF NOT EXISTS idx_audio_assets_created_at_id
    ON audio_assets(created_at ASC, id ASC);

CREATE TABLE IF NOT EXISTS transcription_audio_assets (
    transcription_id BIGINT NOT NULL REFERENCES transcriptions(id) ON DELETE CASCADE,
    audio_asset_id   BIGINT NOT NULL REFERENCES audio_assets(id) ON DELETE CASCADE,
    role             TEXT NOT NULL DEFAULT 'source',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(transcription_id, audio_asset_id)
);

CREATE INDEX IF NOT EXISTS idx_transcription_audio_assets_asset
    ON transcription_audio_assets(audio_asset_id);

CREATE TABLE IF NOT EXISTS quick_note_audio_assets (
    quick_note_id  BIGINT NOT NULL REFERENCES quick_notes(id) ON DELETE CASCADE,
    audio_asset_id BIGINT NOT NULL REFERENCES audio_assets(id) ON DELETE CASCADE,
    role           TEXT NOT NULL DEFAULT 'source',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(quick_note_id, audio_asset_id)
);

CREATE INDEX IF NOT EXISTS idx_quick_note_audio_assets_asset
    ON quick_note_audio_assets(audio_asset_id);

CREATE TABLE IF NOT EXISTS user_dictionary_entries (
    id          BIGSERIAL PRIMARY KEY,
    spoken      TEXT NOT NULL,
    canonical   TEXT NOT NULL,
    language    TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL DEFAULT 'settings',
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    usage_count BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(spoken, canonical, language, source)
);

CREATE INDEX IF NOT EXISTS idx_user_dictionary_language_enabled
    ON user_dictionary_entries(language, enabled, id);

CREATE INDEX IF NOT EXISTS idx_user_dictionary_canonical_lookup
    ON user_dictionary_entries(lower(canonical), language, enabled);

CREATE TABLE IF NOT EXISTS voice_agent_sessions (
    id                  BIGSERIAL PRIMARY KEY,
    title               TEXT NOT NULL DEFAULT '',
    summary             TEXT NOT NULL,
    raw_summary         TEXT NOT NULL DEFAULT '',
    transcript          TEXT NOT NULL DEFAULT '',
    language            TEXT NOT NULL DEFAULT '',
    language_base       TEXT NOT NULL DEFAULT '',
    provider_profile_id TEXT NOT NULL DEFAULT '',
    runtime_kind        TEXT NOT NULL DEFAULT '',
    turns_json          JSONB NOT NULL DEFAULT '[]'::jsonb,
    ideas_json          JSONB NOT NULL DEFAULT '[]'::jsonb,
    decisions_json      JSONB NOT NULL DEFAULT '[]'::jsonb,
    open_questions_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    next_steps_json     JSONB NOT NULL DEFAULT '[]'::jsonb,
    started_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE voice_agent_sessions
    ADD COLUMN IF NOT EXISTS language_base TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_voice_agent_sessions_created_at_id
    ON voice_agent_sessions(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_voice_agent_sessions_language_created_at_id
    ON voice_agent_sessions(language_base, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS voice_agent_session_turns (
    id         BIGSERIAL PRIMARY KEY,
    session_id BIGINT NOT NULL REFERENCES voice_agent_sessions(id) ON DELETE CASCADE,
    turn_index BIGINT NOT NULL,
    role       TEXT NOT NULL,
    text       TEXT NOT NULL,
    created_at TIMESTAMPTZ,
    UNIQUE(session_id, turn_index)
);

CREATE INDEX IF NOT EXISTS idx_voice_agent_session_turns_session
    ON voice_agent_session_turns(session_id, turn_index);

CREATE TABLE IF NOT EXISTS voice_agent_session_summary_items (
    id         BIGSERIAL PRIMARY KEY,
    session_id BIGINT NOT NULL REFERENCES voice_agent_sessions(id) ON DELETE CASCADE,
    item_type  TEXT NOT NULL,
    item_index BIGINT NOT NULL,
    text       TEXT NOT NULL,
    UNIQUE(session_id, item_type, item_index)
);

CREATE INDEX IF NOT EXISTS idx_voice_agent_session_summary_items_session
    ON voice_agent_session_summary_items(session_id, item_type, item_index);

CREATE TABLE IF NOT EXISTS voice_agent_personas (
    id               TEXT PRIMARY KEY,
    display_name     TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    voice            TEXT NOT NULL DEFAULT '',
    locale           TEXT NOT NULL DEFAULT '',
    default_role     TEXT NOT NULL DEFAULT '',
    default_sequence TEXT NOT NULL DEFAULT '',
    tags_json        JSONB NOT NULL DEFAULT '[]'::jsonb,
    metadata_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS voice_agent_roles (
    id                              TEXT PRIMARY KEY,
    display_name                    TEXT NOT NULL,
    system_prompt                   TEXT NOT NULL,
    refinement_prompt               TEXT NOT NULL DEFAULT '',
    locale                          TEXT NOT NULL DEFAULT '',
    vocabulary_hint                 TEXT NOT NULL DEFAULT '',
    tool_allowlist_json             JSONB NOT NULL DEFAULT '[]'::jsonb,
    temperature                     DOUBLE PRECISION NOT NULL DEFAULT 0,
    thinking_enabled                BOOLEAN NOT NULL DEFAULT FALSE,
    thinking_level                  TEXT NOT NULL DEFAULT '',
    include_thoughts                BOOLEAN NOT NULL DEFAULT FALSE,
    thinking_budget                 BIGINT NOT NULL DEFAULT 0,
    automatic_activity_detection    BOOLEAN NOT NULL DEFAULT FALSE,
    vad_start_sensitivity           TEXT NOT NULL DEFAULT '',
    vad_end_sensitivity             TEXT NOT NULL DEFAULT '',
    vad_prefix_padding_ms           BIGINT NOT NULL DEFAULT 0,
    vad_silence_duration_ms         BIGINT NOT NULL DEFAULT 0,
    activity_handling               TEXT NOT NULL DEFAULT '',
    turn_coverage                   TEXT NOT NULL DEFAULT '',
    context_compression_enabled     BOOLEAN NOT NULL DEFAULT FALSE,
    context_compression_trigger_tk  BIGINT NOT NULL DEFAULT 0,
    context_compression_target_tk   BIGINT NOT NULL DEFAULT 0,
    enable_affective_dialog         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS voice_agent_sequences (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    completion   TEXT NOT NULL DEFAULT '',
    max_turns    BIGINT NOT NULL DEFAULT 0,
    steps_json   JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS store_stats (
    scope_id                BIGINT PRIMARY KEY,
    transcriptions_count    BIGINT NOT NULL DEFAULT 0,
    quick_notes_count       BIGINT NOT NULL DEFAULT 0,
    total_words             BIGINT NOT NULL DEFAULT 0,
    total_audio_duration_ms BIGINT NOT NULL DEFAULT 0,
    total_latency_ms        BIGINT NOT NULL DEFAULT 0,
    latency_count           BIGINT NOT NULL DEFAULT 0,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
