CREATE TABLE IF NOT EXISTS voice_agent_sessions (
    id                  BIGSERIAL PRIMARY KEY,
    title               TEXT NOT NULL DEFAULT '',
    summary             TEXT NOT NULL,
    raw_summary         TEXT NOT NULL DEFAULT '',
    transcript          TEXT NOT NULL DEFAULT '',
    language            TEXT NOT NULL DEFAULT '',
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

CREATE INDEX IF NOT EXISTS idx_voice_agent_sessions_created_at
    ON voice_agent_sessions(created_at DESC, id DESC);
