CREATE TABLE IF NOT EXISTS user_dictionary_entries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    spoken      TEXT NOT NULL,
    canonical   TEXT NOT NULL,
    language    TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL DEFAULT 'settings',
    enabled     INTEGER NOT NULL DEFAULT 1,
    usage_count INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(spoken, canonical, language, source)
);

CREATE INDEX IF NOT EXISTS idx_user_dictionary_language
    ON user_dictionary_entries(language, enabled, id);
