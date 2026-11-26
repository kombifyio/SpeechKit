package store

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type voiceTurnAdmission struct {
	Purpose     string `json:"purpose"`
	Issuer      string `json:"issuer"`
	Audience    string `json:"audience"`
	Environment string `json:"environment"`
	Participant string `json:"participant"`
	Route       string `json:"route"`
	Schema      string `json:"schema"`
	DatabaseID  string `json:"database_id"`
	IssuedAt    int64  `json:"issued_at"`
	ExpiresAt   int64  `json:"expires_at"`
}

// The serving process sees only a public key and a deployment-issued proof;
// it never receives the separate subject-rights database credential.
func (s *sqlStore) verifyVoiceTurnAdmission(ctx context.Context, tx *sql.Tx, custody bool) error {
	if s.dialect != dialectPostgres {
		return nil
	}
	databaseID, err := s.verifyVoiceTurnRuntime(ctx, tx)
	if err != nil {
		return err
	}
	envelope, purpose := s.voiceTurnAdmission, "voice-turn-work"
	if custody {
		envelope, purpose = s.voiceTurnCustodyProof, "voice-turn-custody"
	}
	parts := strings.Split(envelope, ".")
	if len(parts) != 2 || len(parts[0]) > 4096 || s.voiceTurnEnvironment == "" || s.voiceTurnIssuer == "" || s.voiceTurnAudience == "" || s.voiceTurnParticipant == "" || s.voiceTurnRoute == "" {
		return errors.New("voice turn receipt admission unavailable")
	}
	decode := base64.RawURLEncoding.DecodeString
	key, e1 := decode(s.voiceTurnAdmissionPublicKey)
	payload, e2 := decode(parts[0])
	sig, e3 := decode(parts[1])
	if e1 != nil || e2 != nil || e3 != nil || len(key) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(key), payload, sig) {
		return errors.New("voice turn receipt admission invalid")
	}
	var c voiceTurnAdmission
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return errors.New("voice turn receipt admission invalid")
	}
	canonical, err := json.Marshal(c)
	if err != nil || !bytes.Equal(canonical, payload) {
		return errors.New("voice turn receipt admission is not canonical")
	}
	now := time.Now().Unix()
	if c.Purpose != purpose || c.Issuer != s.voiceTurnIssuer || c.Audience != s.voiceTurnAudience || c.Environment != s.voiceTurnEnvironment || c.Participant != s.voiceTurnParticipant || c.Route != s.voiceTurnRoute || c.Schema != "postgres:031_voice_turn_receipts" || c.DatabaseID != databaseID || c.IssuedAt <= 0 || c.IssuedAt > now || (!custody && c.ExpiresAt <= now) || c.ExpiresAt <= c.IssuedAt || c.ExpiresAt-c.IssuedAt > 86400 {
		return errors.New("voice turn receipt admission binding rejected")
	}
	return nil
}

// Verify role, effective FORCE RLS and exact policy set independently of the
// admission expiry so already-held receipts remain readable after expiry.
func (s *sqlStore) verifyVoiceTurnRuntime(ctx context.Context, tx *sql.Tx) (string, error) {
	if err := assertPostgresRuntimeRole(ctx, tx); err != nil {
		return "", err
	}
	const owner = "SESSION_USER<>'speechkit_subject_rights'::nameANDowner_user_id=current_setting'speechkit.execution_user_id'::text,trueANDowner_org_id=current_setting'speechkit.execution_org_id'::text,true"
	const subject = "SESSION_USER='speechkit_subject_rights'::nameANDsubject_ref=current_setting'speechkit.subject_ref'::text,true"
	var id string
	var safe bool
	err := tx.QueryRowContext(ctx, `SELECT (SELECT id::text FROM voice_turn_database_identity),
 EXISTS (SELECT 1 FROM schema_migrations WHERE version='postgres:031_voice_turn_receipts')
 AND (SELECT relrowsecurity AND relforcerowsecurity FROM pg_class WHERE oid='voice_turn_receipts'::regclass)
 AND (SELECT count(*)=3 AND bool_and(p.polpermissive AND p.polroles=ARRAY[0::oid] AND (
 (p.polname='voice_turn_owner' AND p.polcmd='*' AND regexp_replace(pg_get_expr(p.polqual,p.polrelid),'[[:space:]()]','','g')=$1 AND regexp_replace(pg_get_expr(p.polwithcheck,p.polrelid),'[[:space:]()]','','g')=$1)
 OR (p.polname='voice_turn_subject_read' AND p.polcmd='r' AND p.polwithcheck IS NULL AND regexp_replace(pg_get_expr(p.polqual,p.polrelid),'[[:space:]()]','','g')=$2)
 OR (p.polname='voice_turn_subject_delete' AND p.polcmd='d' AND p.polwithcheck IS NULL AND regexp_replace(pg_get_expr(p.polqual,p.polrelid),'[[:space:]()]','','g')=$2))) FROM pg_policy p WHERE p.polrelid='voice_turn_receipts'::regclass)
 AND has_table_privilege('voice_turn_feature_used','SELECT') AND has_table_privilege('voice_turn_feature_used','INSERT')
 AND NOT has_table_privilege('voice_turn_feature_used','UPDATE') AND NOT has_table_privilege('voice_turn_feature_used','DELETE')
 AND has_table_privilege('voice_turn_database_identity','SELECT') AND NOT has_table_privilege('voice_turn_database_identity','INSERT')
 AND NOT has_table_privilege('voice_turn_database_identity','UPDATE') AND NOT has_table_privilege('voice_turn_database_identity','DELETE')`, owner, subject).Scan(&id, &safe)
	if err != nil || !safe || id == "" {
		return "", errors.New("voice turn receipt isolation unavailable")
	}
	return id, nil
}
