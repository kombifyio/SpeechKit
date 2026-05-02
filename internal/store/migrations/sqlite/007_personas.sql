-- Migration 007: Voice Agent persona catalog.
--
-- Tables for admin-authored personas, roles, and sequences. TOML-seeded
-- entries remain in-memory only; these tables hold overrides and new
-- entries created via /v1/personas, /v1/roles, /v1/sequences.

CREATE TABLE IF NOT EXISTS voice_agent_personas (
    id             TEXT PRIMARY KEY,
    display_name   TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    voice          TEXT NOT NULL DEFAULT '',
    locale         TEXT NOT NULL DEFAULT '',
    default_role   TEXT NOT NULL DEFAULT '',
    default_sequence TEXT NOT NULL DEFAULT '',
    tags_json      TEXT NOT NULL DEFAULT '[]',
    metadata_json  TEXT NOT NULL DEFAULT '{}',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
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
