ALTER TABLE recording_sessions ADD COLUMN capture_status TEXT NOT NULL DEFAULT 'idle';
ALTER TABLE recording_sessions ADD COLUMN capture_started_at DATETIME;
ALTER TABLE recording_sessions ADD COLUMN capture_paused_at DATETIME;
ALTER TABLE recording_sessions ADD COLUMN capture_stopped_at DATETIME;
ALTER TABLE recording_sessions ADD COLUMN summary_status TEXT NOT NULL DEFAULT 'idle';
ALTER TABLE recording_sessions ADD COLUMN summary_error TEXT NOT NULL DEFAULT '';
ALTER TABLE recording_sessions ADD COLUMN summary_updated_at DATETIME;

CREATE INDEX IF NOT EXISTS idx_recording_sessions_scope_capture
    ON recording_sessions(scope_id, capture_status, updated_at DESC, id DESC);
