package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// VoiceTurnReceipt records only the host-observed dispatch boundary and a
// successful agent return. Unknown never means that a tool did not execute.
type VoiceTurnReceipt struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
	Status    string `json:"status"`
}

type VoiceTurnReceiptStore interface {
	VoiceTurnAdmissionReady(context.Context) error
	BeginVoiceTurn(context.Context, string, string, VoiceTurnReceipt) error
	MarkVoiceTurnDispatched(context.Context, string, string, VoiceTurnReceipt) error
	CompleteVoiceTurn(context.Context, string, string, VoiceTurnReceipt) error
	GetVoiceTurn(context.Context, string, string, string, string) (VoiceTurnReceipt, error)
	ListVoiceTurns(context.Context, string, string, string) ([]VoiceTurnReceipt, error)
}

func (s *sqlStore) VoiceTurnAdmissionReady(ctx context.Context) error {
	if s.dialect == dialectPostgres {
		return s.voiceTurnTx(ctx, "admission-probe", "", true, func(*sql.Tx) error { return nil })
	}
	return nil
}

// A used marker means this runtime cannot truthfully complete subject rights
// until the isolated participant has been admitted for this deployment.
func (s *sqlStore) unhandledVoiceTurnSubjects(ctx context.Context) error {
	if s.dialect != dialectPostgres {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('voice_turn_feature_used') IS NOT NULL`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	var used bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM voice_turn_feature_used)`).Scan(&used); err != nil {
		return err
	}
	if used {
		return s.verifyVoiceTurnAdmission(ctx, tx, true)
	}
	return nil
}

func (s *sqlStore) voiceTurnTx(ctx context.Context, user, org string, requireAdmission bool, work func(*sql.Tx) error) error {
	if user == "" {
		return errors.New("voice turn receipt: owner required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if s.dialect == dialectPostgres {
		if requireAdmission {
			err = s.verifyVoiceTurnAdmission(ctx, tx, false)
		} else {
			_, err = s.verifyVoiceTurnRuntime(ctx, tx)
		}
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `SELECT set_config('speechkit.execution_user_id',$1,true), set_config('speechkit.execution_org_id',$2,true)`, user, org); err != nil {
			return err
		}
	}
	if err = work(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Dispatched means the bound A2A endpoint returned a successful HTTP response;
// it is not proof of a completed action.
func (s *sqlStore) MarkVoiceTurnDispatched(ctx context.Context, user, org string, receipt VoiceTurnReceipt) error {
	return s.voiceTurnTx(ctx, user, org, false, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, s.dialect.rebind(`UPDATE voice_turn_receipts SET status = 'dispatched'
        WHERE owner_user_id = ? AND owner_org_id = ? AND session_id = ? AND turn_id = ? AND status = 'unknown'`), user, org, receipt.SessionID, receipt.TurnID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return errors.New("voice turn receipt: missing dispatch intent")
		}
		return nil
	})
}

func runVoiceTurnReceiptsMigration(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS voice_turn_receipts (
        owner_user_id TEXT NOT NULL, owner_org_id TEXT NOT NULL, session_id TEXT NOT NULL,
        turn_id TEXT NOT NULL, status TEXT NOT NULL, created_at BIGINT NOT NULL,
        PRIMARY KEY (owner_user_id, owner_org_id, session_id, turn_id))`)
	return err
}

func (s *sqlStore) BeginVoiceTurn(ctx context.Context, user, org string, receipt VoiceTurnReceipt) error {
	if user == "" || receipt.SessionID == "" || receipt.TurnID == "" || receipt.Status != "unknown" {
		return errors.New("voice turn receipt: incomplete binding")
	}
	return s.voiceTurnTx(ctx, user, org, true, func(tx *sql.Tx) error {
		if s.dialect == dialectPostgres {
			if s.voiceTurnSubjectReference == nil {
				return errors.New("voice turn subject reference unavailable")
			}
			ref := s.voiceTurnSubjectReference(user)
			if ref == "" {
				return errors.New("voice turn subject reference unavailable")
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO voice_turn_feature_used(id) VALUES (1) ON CONFLICT DO NOTHING`); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO voice_turn_receipts (owner_user_id,owner_org_id,session_id,turn_id,subject_ref,status,created_at) VALUES ($1,$2,$3,$4,$5,'unknown',$6)`, user, org, receipt.SessionID, receipt.TurnID, ref, time.Now().Unix())
			return err
		}
		_, err := tx.ExecContext(ctx, s.dialect.rebind(`INSERT INTO voice_turn_receipts
        (owner_user_id, owner_org_id, session_id, turn_id, status, created_at) VALUES (?, ?, ?, ?, 'unknown', ?)`),
			user, org, receipt.SessionID, receipt.TurnID, time.Now().Unix())
		return err
	})
}

func (s *sqlStore) CompleteVoiceTurn(ctx context.Context, user, org string, receipt VoiceTurnReceipt) error {
	return s.voiceTurnTx(ctx, user, org, false, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, s.dialect.rebind(`UPDATE voice_turn_receipts SET status = 'completed'
		WHERE owner_user_id = ? AND owner_org_id = ? AND session_id = ? AND turn_id = ? AND status IN ('unknown', 'dispatched')`), user, org, receipt.SessionID, receipt.TurnID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return errors.New("voice turn receipt: missing dispatch")
		}
		return nil
	})
}

func (s *sqlStore) GetVoiceTurn(ctx context.Context, user, org, sessionID, turnID string) (VoiceTurnReceipt, error) {
	r := VoiceTurnReceipt{SessionID: sessionID, TurnID: turnID}
	err := s.voiceTurnTx(ctx, user, org, false, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, s.dialect.rebind(`SELECT status FROM voice_turn_receipts
        WHERE owner_user_id = ? AND owner_org_id = ? AND session_id = ? AND turn_id = ?`), user, org, sessionID, turnID).Scan(&r.Status)
	})
	return r, err
}

func (s *sqlStore) ListVoiceTurns(ctx context.Context, user, org, sessionID string) ([]VoiceTurnReceipt, error) {
	result := []VoiceTurnReceipt{}
	err := s.voiceTurnTx(ctx, user, org, false, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, s.dialect.rebind(`SELECT turn_id, status FROM voice_turn_receipts WHERE owner_user_id = ? AND owner_org_id = ? AND session_id = ? ORDER BY created_at, turn_id LIMIT 257`), user, org, sessionID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if len(result) == 256 {
				return errors.New("voice turn receipt: session limit exceeded")
			}
			r := VoiceTurnReceipt{SessionID: sessionID}
			if err := rows.Scan(&r.TurnID, &r.Status); err != nil {
				return err
			}
			result = append(result, r)
		}
		return rows.Err()
	})
	return result, err
}
