CREATE TABLE IF NOT EXISTS transcriptions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    text       TEXT NOT NULL,
    language   TEXT NOT NULL DEFAULT 'de',
    provider   TEXT NOT NULL,
    model      TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    audio_path TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_transcriptions_created_at ON transcriptions(created_at);

CREATE TABLE IF NOT EXISTS quick_notes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    text       TEXT NOT NULL,
    language   TEXT NOT NULL DEFAULT 'de',
    provider   TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    audio_path TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_quick_notes_created_at ON quick_notes(created_at);
