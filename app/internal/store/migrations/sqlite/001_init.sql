CREATE TABLE IF NOT EXISTS transcriptions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    text          TEXT NOT NULL,
    language      TEXT NOT NULL DEFAULT '',
    language_base TEXT NOT NULL DEFAULT '',
    provider      TEXT NOT NULL,
    model         TEXT NOT NULL DEFAULT '',
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    latency_ms    INTEGER NOT NULL DEFAULT 0,
    audio_path    TEXT NOT NULL DEFAULT '',
    word_count    INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_transcriptions_created_at_id
    ON transcriptions(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_transcriptions_language_created_at_id
    ON transcriptions(language_base, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS quick_notes (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    text          TEXT NOT NULL,
    language      TEXT NOT NULL DEFAULT '',
    language_base TEXT NOT NULL DEFAULT '',
    provider      TEXT NOT NULL,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    latency_ms    INTEGER NOT NULL DEFAULT 0,
    audio_path    TEXT NOT NULL DEFAULT '',
    word_count    INTEGER NOT NULL DEFAULT 0,
    pinned        INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_quick_notes_created_at_id
    ON quick_notes(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_quick_notes_pinned_created_at_id
    ON quick_notes(pinned DESC, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_quick_notes_language_created_at_id
    ON quick_notes(language_base, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS audio_assets (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_kind   TEXT NOT NULL DEFAULT '',
    owner_id     INTEGER NOT NULL DEFAULT 0,
    storage_kind TEXT NOT NULL DEFAULT 'local-file',
    path         TEXT NOT NULL,
    mime_type    TEXT NOT NULL DEFAULT 'audio/wav',
    size_bytes   INTEGER NOT NULL DEFAULT 0,
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(owner_kind, owner_id, path)
);

CREATE INDEX IF NOT EXISTS idx_audio_assets_created_at_id
    ON audio_assets(created_at ASC, id ASC);

CREATE TABLE IF NOT EXISTS transcription_audio_assets (
    transcription_id INTEGER NOT NULL REFERENCES transcriptions(id) ON DELETE CASCADE,
    audio_asset_id   INTEGER NOT NULL REFERENCES audio_assets(id) ON DELETE CASCADE,
    role             TEXT NOT NULL DEFAULT 'source',
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (transcription_id, audio_asset_id)
);

CREATE INDEX IF NOT EXISTS idx_transcription_audio_assets_asset
    ON transcription_audio_assets(audio_asset_id);

CREATE TABLE IF NOT EXISTS quick_note_audio_assets (
    quick_note_id  INTEGER NOT NULL REFERENCES quick_notes(id) ON DELETE CASCADE,
    audio_asset_id INTEGER NOT NULL REFERENCES audio_assets(id) ON DELETE CASCADE,
    role           TEXT NOT NULL DEFAULT 'source',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (quick_note_id, audio_asset_id)
);

CREATE INDEX IF NOT EXISTS idx_quick_note_audio_assets_asset
    ON quick_note_audio_assets(audio_asset_id);

CREATE TABLE IF NOT EXISTS user_dictionary_entries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    spoken      TEXT NOT NULL,
    canonical   TEXT NOT NULL,
    language    TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL DEFAULT 'settings',
    enabled     INTEGER NOT NULL DEFAULT 1,
    usage_count INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(spoken, canonical, language, source)
);

CREATE INDEX IF NOT EXISTS idx_user_dictionary_language_enabled
    ON user_dictionary_entries(language, enabled, id);

CREATE INDEX IF NOT EXISTS idx_user_dictionary_canonical_lookup
    ON user_dictionary_entries(lower(canonical), language, enabled);

CREATE TABLE IF NOT EXISTS voice_agent_sessions (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    title               TEXT NOT NULL DEFAULT '',
    summary             TEXT NOT NULL,
    raw_summary         TEXT NOT NULL DEFAULT '',
    transcript          TEXT NOT NULL DEFAULT '',
    language            TEXT NOT NULL DEFAULT '',
    language_base       TEXT NOT NULL DEFAULT '',
    provider_profile_id TEXT NOT NULL DEFAULT '',
    runtime_kind        TEXT NOT NULL DEFAULT '',
    turns_json          TEXT NOT NULL DEFAULT '[]',
    ideas_json          TEXT NOT NULL DEFAULT '[]',
    decisions_json      TEXT NOT NULL DEFAULT '[]',
    open_questions_json TEXT NOT NULL DEFAULT '[]',
    next_steps_json     TEXT NOT NULL DEFAULT '[]',
    started_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_voice_agent_sessions_created_at_id
    ON voice_agent_sessions(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_voice_agent_sessions_language_created_at_id
    ON voice_agent_sessions(language_base, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS voice_agent_session_turns (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL REFERENCES voice_agent_sessions(id) ON DELETE CASCADE,
    turn_index INTEGER NOT NULL,
    role       TEXT NOT NULL,
    text       TEXT NOT NULL,
    created_at DATETIME,
    UNIQUE(session_id, turn_index)
);

CREATE INDEX IF NOT EXISTS idx_voice_agent_session_turns_session
    ON voice_agent_session_turns(session_id, turn_index);

CREATE TABLE IF NOT EXISTS voice_agent_session_summary_items (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL REFERENCES voice_agent_sessions(id) ON DELETE CASCADE,
    item_type  TEXT NOT NULL,
    item_index INTEGER NOT NULL,
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
    tags_json        TEXT NOT NULL DEFAULT '[]',
    metadata_json    TEXT NOT NULL DEFAULT '{}',
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS voice_agent_roles (
    id                              TEXT PRIMARY KEY,
    display_name                    TEXT NOT NULL,
    system_prompt                   TEXT NOT NULL,
    refinement_prompt               TEXT NOT NULL DEFAULT '',
    locale                          TEXT NOT NULL DEFAULT '',
    vocabulary_hint                 TEXT NOT NULL DEFAULT '',
    tool_allowlist_json             TEXT NOT NULL DEFAULT '[]',
    temperature                     REAL NOT NULL DEFAULT 0,
    thinking_enabled                INTEGER NOT NULL DEFAULT 0,
    thinking_level                  TEXT NOT NULL DEFAULT '',
    include_thoughts                INTEGER NOT NULL DEFAULT 0,
    thinking_budget                 INTEGER NOT NULL DEFAULT 0,
    automatic_activity_detection    INTEGER NOT NULL DEFAULT 0,
    vad_start_sensitivity           TEXT NOT NULL DEFAULT '',
    vad_end_sensitivity             TEXT NOT NULL DEFAULT '',
    vad_prefix_padding_ms           INTEGER NOT NULL DEFAULT 0,
    vad_silence_duration_ms         INTEGER NOT NULL DEFAULT 0,
    activity_handling               TEXT NOT NULL DEFAULT '',
    turn_coverage                   TEXT NOT NULL DEFAULT '',
    context_compression_enabled     INTEGER NOT NULL DEFAULT 0,
    context_compression_trigger_tk  INTEGER NOT NULL DEFAULT 0,
    context_compression_target_tk   INTEGER NOT NULL DEFAULT 0,
    enable_affective_dialog         INTEGER NOT NULL DEFAULT 0,
    created_at                      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS voice_agent_sequences (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    completion   TEXT NOT NULL DEFAULT '',
    max_turns    INTEGER NOT NULL DEFAULT 0,
    steps_json   TEXT NOT NULL DEFAULT '[]',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS store_stats (
    scope_id                INTEGER PRIMARY KEY,
    transcriptions_count    INTEGER NOT NULL DEFAULT 0,
    quick_notes_count       INTEGER NOT NULL DEFAULT 0,
    total_words             INTEGER NOT NULL DEFAULT 0,
    total_audio_duration_ms INTEGER NOT NULL DEFAULT 0,
    total_latency_ms        INTEGER NOT NULL DEFAULT 0,
    latency_count           INTEGER NOT NULL DEFAULT 0,
    updated_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
