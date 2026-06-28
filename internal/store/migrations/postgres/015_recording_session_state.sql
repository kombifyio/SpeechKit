ALTER TABLE recording_sessions
    ADD COLUMN IF NOT EXISTS capture_status TEXT NOT NULL DEFAULT 'idle',
    ADD COLUMN IF NOT EXISTS capture_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS capture_paused_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS capture_stopped_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS summary_status TEXT NOT NULL DEFAULT 'idle',
    ADD COLUMN IF NOT EXISTS summary_error TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS summary_updated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_recording_sessions_scope_capture
    ON recording_sessions(scope_id, capture_status, updated_at DESC, id DESC);
