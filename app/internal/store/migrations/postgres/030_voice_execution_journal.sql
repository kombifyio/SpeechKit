SET LOCAL lock_timeout = '1000ms';
SET LOCAL statement_timeout = '5000ms';

-- Execution accounting metadata and durable receipt obligations.
CREATE TABLE voice_execution_attempts (
    attempt_id TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL,
    owner_org_id TEXT NOT NULL,
    document BYTEA NOT NULL,
    phase TEXT NOT NULL CHECK (phase IN ('prepared', 'active', 'finalizing', 'finalized')),
    expires_at_ms BIGINT NOT NULL
);
ALTER TABLE voice_execution_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE voice_execution_attempts FORCE ROW LEVEL SECURITY;
CREATE POLICY voice_execution_attempt_owner ON voice_execution_attempts
    USING (owner_user_id = current_setting('speechkit.execution_user_id', true)
       AND owner_org_id = current_setting('speechkit.execution_org_id', true))
    WITH CHECK (owner_user_id = current_setting('speechkit.execution_user_id', true)
       AND owner_org_id = current_setting('speechkit.execution_org_id', true));


-- One reservation/receipt may belong to only one attempt, even across tenants.
CREATE TABLE voice_execution_keys (
    key TEXT PRIMARY KEY,
    attempt_id TEXT NOT NULL,
    owner_user_id TEXT NOT NULL,
    owner_org_id TEXT NOT NULL
);
ALTER TABLE voice_execution_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE voice_execution_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY voice_execution_key_owner ON voice_execution_keys
    USING (owner_user_id = current_setting('speechkit.execution_user_id', true)
       AND owner_org_id = current_setting('speechkit.execution_org_id', true))
    WITH CHECK (owner_user_id = current_setting('speechkit.execution_user_id', true)
       AND owner_org_id = current_setting('speechkit.execution_org_id', true));

-- Recovery routing only: no document, audio, receipt body or receipt capability.
-- The migration grant lane admits the non-owning runtime role; no public access.
CREATE TABLE voice_execution_pending (
    attempt_id TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL,
    owner_org_id TEXT NOT NULL,
    due_at_ms BIGINT NOT NULL CHECK (due_at_ms >= 0)
);
REVOKE ALL ON voice_execution_pending FROM PUBLIC;
CREATE INDEX voice_execution_pending_due ON voice_execution_pending (due_at_ms, attempt_id);
