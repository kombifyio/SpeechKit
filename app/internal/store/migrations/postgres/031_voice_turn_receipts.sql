SET LOCAL lock_timeout = '1000ms';
SET LOCAL statement_timeout = '5000ms';
CREATE TABLE voice_turn_receipts (
    owner_user_id TEXT NOT NULL, owner_org_id TEXT NOT NULL, session_id TEXT NOT NULL,
    turn_id TEXT NOT NULL, subject_ref TEXT NOT NULL, status TEXT NOT NULL CHECK (status IN ('unknown', 'dispatched', 'completed')),
    created_at BIGINT NOT NULL, PRIMARY KEY (owner_user_id, owner_org_id, session_id, turn_id)
);
ALTER TABLE voice_turn_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE voice_turn_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY voice_turn_owner ON voice_turn_receipts
    USING (session_user <> 'speechkit_subject_rights' AND owner_user_id = current_setting('speechkit.execution_user_id', true)
       AND owner_org_id = current_setting('speechkit.execution_org_id', true))
    WITH CHECK (session_user <> 'speechkit_subject_rights' AND owner_user_id = current_setting('speechkit.execution_user_id', true)
       AND owner_org_id = current_setting('speechkit.execution_org_id', true));
CREATE POLICY voice_turn_subject_read ON voice_turn_receipts FOR SELECT
    USING (session_user = 'speechkit_subject_rights' AND subject_ref = current_setting('speechkit.subject_ref', true));
CREATE POLICY voice_turn_subject_delete ON voice_turn_receipts FOR DELETE
    USING (session_user = 'speechkit_subject_rights' AND subject_ref = current_setting('speechkit.subject_ref', true));
CREATE INDEX voice_turn_subject_ref ON voice_turn_receipts(subject_ref);
-- A non-user marker: once any receipt was admitted, a missing DSAR principal
-- blocks subject rights instead of silently omitting rows after a restart.
CREATE TABLE voice_turn_feature_used (id INTEGER PRIMARY KEY CHECK (id = 1));
REVOKE ALL ON voice_turn_feature_used FROM PUBLIC;
CREATE TABLE voice_turn_database_identity (id UUID PRIMARY KEY DEFAULT gen_random_uuid());
INSERT INTO voice_turn_database_identity DEFAULT VALUES;
REVOKE ALL ON voice_turn_database_identity FROM PUBLIC;

