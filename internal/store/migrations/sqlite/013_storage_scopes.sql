CREATE TABLE IF NOT EXISTS storage_scopes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_key   TEXT NOT NULL UNIQUE,
    install_id  TEXT NOT NULL DEFAULT '',
    device_id   TEXT NOT NULL DEFAULT '',
    user_id     TEXT NOT NULL DEFAULT '',
    tenant_id   TEXT NOT NULL DEFAULT '',
    labels_json TEXT NOT NULL DEFAULT '{}',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO storage_scopes
    (id, scope_key, install_id, labels_json)
VALUES
    (1, 'install:local', 'local', '{}');

CREATE TABLE IF NOT EXISTS store_stats (
    scope_id                INTEGER PRIMARY KEY REFERENCES storage_scopes(id) ON DELETE CASCADE,
    transcriptions_count    INTEGER NOT NULL DEFAULT 0,
    quick_notes_count       INTEGER NOT NULL DEFAULT 0,
    total_words             INTEGER NOT NULL DEFAULT 0,
    total_audio_duration_ms INTEGER NOT NULL DEFAULT 0,
    total_latency_ms        INTEGER NOT NULL DEFAULT 0,
    latency_count           INTEGER NOT NULL DEFAULT 0,
    updated_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

