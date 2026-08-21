-- A meeting a user pins is kept regardless of the retention window. Retention
-- exists so a machine does not accumulate transcripts of every call forever;
-- the one meeting someone actually needs to keep should not be collateral.
ALTER TABLE recording_sessions ADD COLUMN retention_pinned INTEGER NOT NULL DEFAULT 0;
