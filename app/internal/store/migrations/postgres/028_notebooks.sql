CREATE TABLE IF NOT EXISTS notebooks (
    id               BIGSERIAL PRIMARY KEY,
    scope_id         BIGINT NOT NULL DEFAULT 1 REFERENCES storage_scopes(id) ON DELETE CASCADE,
    display_name     TEXT NOT NULL DEFAULT '',
    destination_kind TEXT NOT NULL DEFAULT 'onenote',
    notebook_id      TEXT NOT NULL DEFAULT '',
    section_id       TEXT NOT NULL DEFAULT '',
    display_path     TEXT NOT NULL DEFAULT '',
    last_synced_at   TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notebooks_scope_created
    ON notebooks(scope_id, created_at DESC, id DESC);

ALTER TABLE recording_sessions
    ADD COLUMN IF NOT EXISTS notebook_id BIGINT REFERENCES notebooks(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_recording_sessions_notebook
    ON recording_sessions(notebook_id);
