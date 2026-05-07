package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

var _ VoiceAgentSessionStore = (*SQLiteStore)(nil)
var _ VoiceAgentSessionStore = (*PostgresStore)(nil)

func (s *SQLiteStore) SaveVoiceAgentSession(ctx context.Context, session VoiceAgentSession) (int64, error) {
	session = normalizeVoiceAgentSession(session)
	j, err := marshalVoiceAgentSessionJSON(session)
	if err != nil {
		return 0, err
	}
	owner, _ := RecordOwnerFromContext(ctx)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback is harmless after commit and not actionable.

	result, err := tx.ExecContext(ctx,
		`INSERT INTO voice_agent_sessions (
			title, summary, raw_summary, transcript, language, provider_profile_id, runtime_kind,
			turns_json, ideas_json, decisions_json, open_questions_json, next_steps_json, started_at, ended_at,
			owner_user_id, owner_org_id, owner_source
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.Summary.Title,
		session.Summary.Summary,
		session.Summary.RawText,
		session.Transcript,
		session.Language,
		session.ProviderProfileID,
		session.RuntimeKind,
		j.Turns,
		j.Ideas,
		j.Decisions,
		j.Questions,
		j.Steps,
		session.StartedAt,
		session.EndedAt,
		owner.UserID,
		owner.OrgID,
		owner.Source,
	)
	if err != nil {
		return 0, fmt.Errorf("insert voice agent session: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err = replaceVoiceAgentSessionChildren(ctx, tx, "sqlite", id, session); err != nil {
		return 0, fmt.Errorf("insert voice agent session children: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *SQLiteStore) ListVoiceAgentSessions(ctx context.Context, opts ListOpts) ([]VoiceAgentSession, error) {
	limit, offset := normalizedListPagination(opts)

	query := `SELECT id, title, summary, raw_summary, transcript, language, provider_profile_id, runtime_kind,
			turns_json, ideas_json, decisions_json, open_questions_json, next_steps_json, started_at, ended_at, created_at,
			COALESCE(owner_user_id, ''), COALESCE(owner_org_id, ''), COALESCE(owner_source, '')
		 FROM voice_agent_sessions`
	args := make([]any, 0, 4)
	clauses := make([]string, 0, 2)
	clauses, args = appendSQLiteNormalizedLanguageFilter(clauses, args, opts.Language)
	clauses, args = appendSQLiteOwnerFilter(clauses, args, opts)
	if !opts.After.IsZero() {
		clauses = append(clauses, "created_at > ?")
		args = append(args, sqliteTime(opts.After))
	}
	query += buildWhereClause(clauses) // #nosec G202 -- audited choke-point; see SECURITY INVARIANT in query_filters.go.
	query += " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, //nolint:rowserrcheck // rows.Err() is checked inside scanVoiceAgentSessions
		query, args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	return scanVoiceAgentSessions(rows)
}

func (s *SQLiteStore) GetVoiceAgentSession(ctx context.Context, id int64) (*VoiceAgentSession, error) {
	rows, err := s.db.QueryContext(ctx, //nolint:rowserrcheck // rows.Err() is checked inside scanVoiceAgentSessions
		`SELECT id, title, summary, raw_summary, transcript, language, provider_profile_id, runtime_kind,
			turns_json, ideas_json, decisions_json, open_questions_json, next_steps_json, started_at, ended_at, created_at,
			COALESCE(owner_user_id, ''), COALESCE(owner_org_id, ''), COALESCE(owner_source, '')
		 FROM voice_agent_sessions WHERE id = ?`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable
	sessions, err := scanVoiceAgentSessions(rows)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, sql.ErrNoRows
	}
	return &sessions[0], nil
}

func (s *PostgresStore) SaveVoiceAgentSession(ctx context.Context, session VoiceAgentSession) (int64, error) {
	session = normalizeVoiceAgentSession(session)
	j, err := marshalVoiceAgentSessionJSON(session)
	if err != nil {
		return 0, err
	}
	owner, _ := RecordOwnerFromContext(ctx)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback is harmless after commit and not actionable.

	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO voice_agent_sessions (
			title, summary, raw_summary, transcript, language, provider_profile_id, runtime_kind,
			turns_json, ideas_json, decisions_json, open_questions_json, next_steps_json, started_at, ended_at,
			owner_user_id, owner_org_id, owner_source
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10::jsonb, $11::jsonb, $12::jsonb, $13, $14, $15, $16, $17)
		RETURNING id`,
		session.Summary.Title,
		session.Summary.Summary,
		session.Summary.RawText,
		session.Transcript,
		session.Language,
		session.ProviderProfileID,
		session.RuntimeKind,
		j.Turns,
		j.Ideas,
		j.Decisions,
		j.Questions,
		j.Steps,
		session.StartedAt,
		session.EndedAt,
		owner.UserID,
		owner.OrgID,
		owner.Source,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert voice agent session: %w", err)
	}
	if err = replaceVoiceAgentSessionChildren(ctx, tx, "postgres", id, session); err != nil {
		return 0, fmt.Errorf("insert voice agent session children: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *PostgresStore) ListVoiceAgentSessions(ctx context.Context, opts ListOpts) ([]VoiceAgentSession, error) {
	limit, offset := normalizedListPagination(opts)

	query := `SELECT id, title, summary, raw_summary, transcript, language, provider_profile_id, runtime_kind,
			turns_json::text, ideas_json::text, decisions_json::text, open_questions_json::text, next_steps_json::text,
			started_at, ended_at, created_at,
			COALESCE(owner_user_id, ''), COALESCE(owner_org_id, ''), COALESCE(owner_source, '')
		 FROM voice_agent_sessions`
	args := make([]any, 0, 4)
	clauses := make([]string, 0, 2)
	clauses, args = appendPostgresNormalizedLanguageFilter(clauses, args, opts.Language)
	clauses, args = appendPostgresOwnerFilter(clauses, args, opts)
	if !opts.After.IsZero() {
		args = append(args, opts.After.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at > $%d", len(args)))
	}
	query += buildWhereClause(clauses) // #nosec G202 -- audited choke-point; see SECURITY INVARIANT in query_filters.go.
	args = append(args, limit)
	limitParam := len(args)
	args = append(args, offset)
	offsetParam := len(args)
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d", limitParam, offsetParam) // #nosec G202 -- limit/offset positions are derived from argument indexes.

	rows, err := s.db.QueryContext(ctx, //nolint:rowserrcheck // rows.Err() is checked inside scanVoiceAgentSessions
		query, args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	return scanVoiceAgentSessions(rows)
}

func (s *PostgresStore) GetVoiceAgentSession(ctx context.Context, id int64) (*VoiceAgentSession, error) {
	rows, err := s.db.QueryContext(ctx, //nolint:rowserrcheck // rows.Err() is checked inside scanVoiceAgentSessions
		`SELECT id, title, summary, raw_summary, transcript, language, provider_profile_id, runtime_kind,
			turns_json::text, ideas_json::text, decisions_json::text, open_questions_json::text, next_steps_json::text,
			started_at, ended_at, created_at,
			COALESCE(owner_user_id, ''), COALESCE(owner_org_id, ''), COALESCE(owner_source, '')
		 FROM voice_agent_sessions WHERE id = $1`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable
	sessions, err := scanVoiceAgentSessions(rows)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, sql.ErrNoRows
	}
	return &sessions[0], nil
}

func replaceVoiceAgentSessionChildren(ctx context.Context, db execContexter, dialect string, sessionID int64, session VoiceAgentSession) error {
	deleteTurns := `DELETE FROM voice_agent_session_turns WHERE session_id = ?`
	deleteItems := `DELETE FROM voice_agent_session_summary_items WHERE session_id = ?`
	if dialect == "postgres" {
		deleteTurns = `DELETE FROM voice_agent_session_turns WHERE session_id = $1`
		deleteItems = `DELETE FROM voice_agent_session_summary_items WHERE session_id = $1`
	}
	if _, err := db.ExecContext(ctx, deleteTurns, sessionID); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, deleteItems, sessionID); err != nil {
		return err
	}

	turnQuery := `INSERT INTO voice_agent_session_turns
		(session_id, turn_index, role, text, created_at)
		VALUES (?, ?, ?, ?, ?)`
	itemQuery := `INSERT INTO voice_agent_session_summary_items
		(session_id, item_type, item_index, text)
		VALUES (?, ?, ?, ?)`
	if dialect == "postgres" {
		turnQuery = `INSERT INTO voice_agent_session_turns
			(session_id, turn_index, role, text, created_at)
			VALUES ($1, $2, $3, $4, $5)`
		itemQuery = `INSERT INTO voice_agent_session_summary_items
			(session_id, item_type, item_index, text)
			VALUES ($1, $2, $3, $4)`
	}

	for i, turn := range session.Turns {
		if _, err := db.ExecContext(ctx, turnQuery,
			sessionID,
			i,
			strings.TrimSpace(turn.Role),
			strings.TrimSpace(turn.Text),
			nullableVoiceAgentTime(turn.CreatedAt),
		); err != nil {
			return err
		}
	}

	if err := insertVoiceAgentSummaryItems(ctx, db, itemQuery, sessionID, "idea", session.Summary.Ideas); err != nil {
		return err
	}
	if err := insertVoiceAgentSummaryItems(ctx, db, itemQuery, sessionID, "decision", session.Summary.Decisions); err != nil {
		return err
	}
	if err := insertVoiceAgentSummaryItems(ctx, db, itemQuery, sessionID, "open_question", session.Summary.OpenQuestions); err != nil {
		return err
	}
	return insertVoiceAgentSummaryItems(ctx, db, itemQuery, sessionID, "next_step", session.Summary.NextSteps)
}

func insertVoiceAgentSummaryItems(ctx context.Context, db execContexter, query string, sessionID int64, itemType string, values []string) error {
	for i, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, query, sessionID, itemType, i, text); err != nil {
			return err
		}
	}
	return nil
}

func nullableVoiceAgentTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

type voiceAgentSessionRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Columns() ([]string, error)
}

func scanVoiceAgentSessions(rows voiceAgentSessionRows) ([]VoiceAgentSession, error) {
	columns, _ := rows.Columns()
	hasOwner := len(columns) >= 19
	sessions := make([]VoiceAgentSession, 0)
	for rows.Next() {
		var (
			session       VoiceAgentSession
			turnsJSON     string
			ideasJSON     string
			decisionsJSON string
			questionsJSON string
			stepsJSON     string
		)
		dest := []any{
			&session.ID,
			&session.Summary.Title,
			&session.Summary.Summary,
			&session.Summary.RawText,
			&session.Transcript,
			&session.Language,
			&session.ProviderProfileID,
			&session.RuntimeKind,
			&turnsJSON,
			&ideasJSON,
			&decisionsJSON,
			&questionsJSON,
			&stepsJSON,
			&session.StartedAt,
			&session.EndedAt,
			&session.CreatedAt,
		}
		if hasOwner {
			dest = append(dest, &session.OwnerUserID, &session.OwnerOrgID, &session.OwnerSource)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		session.Turns = unmarshalVoiceAgentTurns(turnsJSON)
		session.Summary.Ideas = unmarshalStringSlice(ideasJSON)
		session.Summary.Decisions = unmarshalStringSlice(decisionsJSON)
		session.Summary.OpenQuestions = unmarshalStringSlice(questionsJSON)
		session.Summary.NextSteps = unmarshalStringSlice(stepsJSON)
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func normalizeVoiceAgentSession(session VoiceAgentSession) VoiceAgentSession {
	now := time.Now().UTC()
	session.Summary.Title = strings.TrimSpace(session.Summary.Title)
	session.Summary.Summary = strings.TrimSpace(session.Summary.Summary)
	session.Summary.RawText = strings.TrimSpace(session.Summary.RawText)
	session.Transcript = strings.TrimSpace(session.Transcript)
	session.Language = strings.TrimSpace(session.Language)
	session.ProviderProfileID = strings.TrimSpace(session.ProviderProfileID)
	session.RuntimeKind = strings.TrimSpace(session.RuntimeKind)
	if session.StartedAt.IsZero() {
		session.StartedAt = now
	}
	if session.EndedAt.IsZero() {
		session.EndedAt = now
	}
	if session.Summary.Title == "" {
		session.Summary.Title = deriveVoiceAgentSessionTitle(session.Summary.Summary)
	}
	if session.Summary.RawText == "" {
		session.Summary.RawText = session.Summary.Summary
	}
	return session
}

// voiceAgentSessionJSON bundles the five JSON-serialized facets of a
// VoiceAgentSession into a single value so callers do not have to unpack
// five return values.
type voiceAgentSessionJSON struct {
	Turns     string
	Ideas     string
	Decisions string
	Questions string
	Steps     string
}

func marshalVoiceAgentSessionJSON(session VoiceAgentSession) (voiceAgentSessionJSON, error) {
	var j voiceAgentSessionJSON
	var err error
	j.Turns, err = marshalJSON(session.Turns)
	if err != nil {
		return voiceAgentSessionJSON{}, fmt.Errorf("marshal voice agent turns: %w", err)
	}
	j.Ideas, err = marshalJSON(session.Summary.Ideas)
	if err != nil {
		return voiceAgentSessionJSON{}, fmt.Errorf("marshal voice agent ideas: %w", err)
	}
	j.Decisions, err = marshalJSON(session.Summary.Decisions)
	if err != nil {
		return voiceAgentSessionJSON{}, fmt.Errorf("marshal voice agent decisions: %w", err)
	}
	j.Questions, err = marshalJSON(session.Summary.OpenQuestions)
	if err != nil {
		return voiceAgentSessionJSON{}, fmt.Errorf("marshal voice agent open questions: %w", err)
	}
	j.Steps, err = marshalJSON(session.Summary.NextSteps)
	if err != nil {
		return voiceAgentSessionJSON{}, fmt.Errorf("marshal voice agent next steps: %w", err)
	}
	return j, nil
}

func marshalJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if string(raw) == "null" {
		return "[]", nil
	}
	return string(raw), nil
}

func unmarshalVoiceAgentTurns(raw string) []VoiceAgentTurn {
	var turns []VoiceAgentTurn
	if err := json.Unmarshal([]byte(firstNonEmptyStoreString(raw, "[]")), &turns); err != nil {
		return nil
	}
	return turns
}

func unmarshalStringSlice(raw string) []string {
	var values []string
	if err := json.Unmarshal([]byte(firstNonEmptyStoreString(raw, "[]")), &values); err != nil {
		return nil
	}
	return values
}

func firstNonEmptyStoreString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func deriveVoiceAgentSessionTitle(summary string) string {
	words := strings.Fields(summary)
	if len(words) == 0 {
		return "Voice Agent session"
	}
	if len(words) > 8 {
		words = words[:8]
	}
	return strings.Join(words, " ")
}
