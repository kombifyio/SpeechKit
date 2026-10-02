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

