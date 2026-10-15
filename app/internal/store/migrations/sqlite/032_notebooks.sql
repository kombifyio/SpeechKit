CREATE TABLE IF NOT EXISTS notebooks (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_id         INTEGER NOT NULL DEFAULT 1 REFERENCES storage_scopes(id) ON DELETE CASCADE,
    display_name     TEXT NOT NULL DEFAULT '',
    destination_kind TEXT NOT NULL DEFAULT 'onenote',
    notebook_id      TEXT NOT NULL DEFAULT '',
    section_id       TEXT NOT NULL DEFAULT '',
    display_path     TEXT NOT NULL DEFAULT '',
    last_synced_at   DATETIME,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_notebooks_scope_created
    ON notebooks(scope_id, created_at DESC, id DESC);
