-- v0.37.5: wake-word activation training-data collection.
--
-- One row per uploaded activation clip. Audio bytes live on the
-- filesystem under <server.training_data.audio_dir>/<org>/<user>/<id>.wav;
-- this table stores only metadata + the relative path so the audio
-- subsystem can prune orphans during retention sweeps.
--
-- Multi-tenant scoped via owner_user_id / owner_org_id — every read
-- and write goes through the existing Identity{User,Org} context so
-- one tenant cannot see another's recordings even when the same
-- audio_dir is shared across organisations.

CREATE TABLE IF NOT EXISTS wakeword_activations (
    id                 TEXT PRIMARY KEY,
    owner_user_id      TEXT NOT NULL,
    owner_org_id       TEXT NOT NULL,
    phrase_id          TEXT NOT NULL,
    phrase             TEXT NOT NULL DEFAULT '',
    backend            TEXT NOT NULL DEFAULT '',
    score              REAL NOT NULL DEFAULT 0,
    captured_at        DATETIME NOT NULL,
    uploaded_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    label              TEXT NOT NULL DEFAULT '',
    audio_path         TEXT NOT NULL,
    audio_bytes        INTEGER NOT NULL DEFAULT 0,
    sample_rate        INTEGER NOT NULL DEFAULT 16000,
    pre_roll_ms        INTEGER NOT NULL DEFAULT 0,
    post_roll_ms       INTEGER NOT NULL DEFAULT 0,
    metadata_json      TEXT NOT NULL DEFAULT '{}'
);

-- Per-owner listing index (newest first). This is the hot read path
-- for the v0.37.6 Settings UI activation list + the future training
-- export tooling.
CREATE INDEX IF NOT EXISTS idx_wakeword_activations_owner
    ON wakeword_activations(owner_org_id, owner_user_id, captured_at DESC);

-- Per-phrase index supports the training pipeline that bundles all
-- recordings of one phrase into a single dataset directory.
CREATE INDEX IF NOT EXISTS idx_wakeword_activations_phrase
    ON wakeword_activations(phrase_id, captured_at DESC);

-- Label index lets the admin dashboard surface "unreviewed
-- captures" (label = '') vs "labeled" (label != '') quickly.
CREATE INDEX IF NOT EXISTS idx_wakeword_activations_label
    ON wakeword_activations(label, captured_at DESC);
