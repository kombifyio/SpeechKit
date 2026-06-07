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

func (s *sqlStore) SaveVoiceAgentSession(ctx context.Context, session VoiceAgentSession) (int64, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	owner, _ := RecordOwnerFromContext(ctx)
	session = normalizeVoiceAgentSession(session)
	j, err := marshalVoiceAgentSessionJSON(session)
	if err != nil {
		return 0, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback is harmless after commit and not actionable.

	jc := s.dialect.jsonbInsert()
	id, err := s.dialect.insertReturningID(ctx, tx,
		`INSERT INTO voice_agent_sessions (
			scope_id, title, summary, raw_summary, transcript, language, language_base, provider_profile_id, runtime_kind,
			turns_json, ideas_json, decisions_json, open_questions_json, next_steps_json, started_at, ended_at,
			owner_user_id, owner_org_id, owner_source
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?`+jc+`, ?`+jc+`, ?`+jc+`, ?`+jc+`, ?`+jc+`, ?, ?, ?, ?, ?)`,
		scopeID,
		session.Summary.Title,
		session.Summary.Summary,
		session.Summary.RawText,
		session.Transcript,
		session.Language,
		normalizeDictionaryLanguage(session.Language),
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
	if err = replaceVoiceAgentSessionChildren(ctx, tx, s.dialect, id, session); err != nil {
		return 0, fmt.Errorf("insert voice agent session children: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *sqlStore) ListVoiceAgentSessions(ctx context.Context, opts ListOpts) ([]VoiceAgentSession, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	limit, offset := normalizedListPagination(opts)

	transcriptSelect := "''"
	if voiceAgentListIncludesTranscript(opts) {
		transcriptSelect = "transcript"
	}
	query := `SELECT id, title, summary, ` + transcriptSelect + `, language, provider_profile_id, runtime_kind, started_at, ended_at, created_at,
			COALESCE(owner_user_id, ''), COALESCE(owner_org_id, ''), COALESCE(owner_source, '')
		 FROM voice_agent_sessions`
	args := []any{scopeID}
	clauses := []string{"scope_id = ?"}
	clauses, args = appendNormalizedLanguageFilter(clauses, args, opts.Language)
	clauses, args = appendOwnerFilter(clauses, args, opts)
	if !opts.After.IsZero() {
		clauses = append(clauses, "created_at > ?")
		args = append(args, s.dialect.timeArg(opts.After))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ") // #nosec G202 -- clauses are fixed internal snippets; values are parameterized.
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(query), args...) //nolint:rowserrcheck // rows.Err() is checked inside scanVoiceAgentSessionList
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	return scanVoiceAgentSessionList(rows)
}

func (s *sqlStore) GetVoiceAgentSession(ctx context.Context, id int64) (*VoiceAgentSession, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, s.dialect.rebind(
		`SELECT id, title, summary, raw_summary, transcript, language, provider_profile_id, runtime_kind, started_at, ended_at, created_at,
			COALESCE(owner_user_id, ''), COALESCE(owner_org_id, ''), COALESCE(owner_source, '')
		 FROM voice_agent_sessions WHERE id = ? AND scope_id = ?`),
		id, scopeID,
	)
	session, err := scanVoiceAgentSessionDetailRow(row)
	if err != nil {
		return nil, err
	}
	if err := loadVoiceAgentSessionChildren(ctx, s.db, s.dialect, session); err != nil {
		return nil, err
	}
	return session, nil
}

func replaceVoiceAgentSessionChildren(ctx context.Context, db execContexter, d sqlDialect, sessionID int64, session VoiceAgentSession) error {
	if _, err := db.ExecContext(ctx, d.rebind(`DELETE FROM voice_agent_session_turns WHERE session_id = ?`), sessionID); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, d.rebind(`DELETE FROM voice_agent_session_summary_items WHERE session_id = ?`), sessionID); err != nil {
		return err
	}

	turnQuery := d.rebind(`INSERT INTO voice_agent_session_turns
		(session_id, turn_index, role, text, created_at)
		VALUES (?, ?, ?, ?, ?)`)
	itemQuery := d.rebind(`INSERT INTO voice_agent_session_summary_items
		(session_id, item_type, item_index, text)
		VALUES (?, ?, ?, ?)`)

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
}

type voiceAgentSessionRow interface {
	Scan(dest ...any) error
}

func voiceAgentListIncludesTranscript(opts ListOpts) bool {
	return strings.TrimSpace(opts.OwnerUserID) != "" || strings.TrimSpace(opts.OwnerOrgID) != ""
}

func scanVoiceAgentSessionList(rows voiceAgentSessionRows) ([]VoiceAgentSession, error) {
	sessions := make([]VoiceAgentSession, 0)
	for rows.Next() {
		var session VoiceAgentSession
		if err := rows.Scan(
			&session.ID,
			&session.Summary.Title,
			&session.Summary.Summary,
			&session.Transcript,
			&session.Language,
			&session.ProviderProfileID,
			&session.RuntimeKind,
			&session.StartedAt,
			&session.EndedAt,
			&session.CreatedAt,
			&session.OwnerUserID,
			&session.OwnerOrgID,
			&session.OwnerSource,
		); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func scanVoiceAgentSessionDetailRow(row voiceAgentSessionRow) (*VoiceAgentSession, error) {
	var session VoiceAgentSession
	if err := row.Scan(
		&session.ID,
		&session.Summary.Title,
		&session.Summary.Summary,
		&session.Summary.RawText,
		&session.Transcript,
		&session.Language,
		&session.ProviderProfileID,
		&session.RuntimeKind,
		&session.StartedAt,
		&session.EndedAt,
		&session.CreatedAt,
		&session.OwnerUserID,
		&session.OwnerOrgID,
		&session.OwnerSource,
	); err != nil {
		return nil, err
	}
	return &session, nil
}

func loadVoiceAgentSessionChildren(ctx context.Context, db *sql.DB, d sqlDialect, session *VoiceAgentSession) error {
	turnRows, err := db.QueryContext(ctx, d.rebind(`SELECT role, text, created_at FROM voice_agent_session_turns WHERE session_id = ? ORDER BY turn_index ASC`), session.ID)
	if err != nil {
		return err
	}
	defer turnRows.Close() //nolint:errcheck // deferred rows close, error not actionable.
	for turnRows.Next() {
		var turn VoiceAgentTurn
		var createdAt sql.NullTime
		if err := turnRows.Scan(&turn.Role, &turn.Text, &createdAt); err != nil {
			return err
		}
		if createdAt.Valid {
			turn.CreatedAt = createdAt.Time
		}
		session.Turns = append(session.Turns, turn)
	}
	if err := turnRows.Err(); err != nil {
		return err
	}

	itemRows, err := db.QueryContext(ctx, d.rebind(`SELECT item_type, text FROM voice_agent_session_summary_items WHERE session_id = ? ORDER BY item_type ASC, item_index ASC`), session.ID)
	if err != nil {
		return err
	}
	defer itemRows.Close() //nolint:errcheck // deferred rows close, error not actionable.
	for itemRows.Next() {
		var itemType string
		var text string
		if err := itemRows.Scan(&itemType, &text); err != nil {
			return err
		}
		switch itemType {
		case "idea":
			session.Summary.Ideas = append(session.Summary.Ideas, text)
		case "decision":
			session.Summary.Decisions = append(session.Summary.Decisions, text)
		case "open_question":
			session.Summary.OpenQuestions = append(session.Summary.OpenQuestions, text)
		case "next_step":
			session.Summary.NextSteps = append(session.Summary.NextSteps, text)
		}
	}
	return itemRows.Err()
}

func scanVoiceAgentSessions(rows voiceAgentSessionRows) ([]VoiceAgentSession, error) {
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
		if err := rows.Scan(
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
		); err != nil {
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
