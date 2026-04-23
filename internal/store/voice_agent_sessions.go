package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

var _ VoiceAgentSessionStore = (*SQLiteStore)(nil)
var _ VoiceAgentSessionStore = (*PostgresStore)(nil)

func (s *SQLiteStore) SaveVoiceAgentSession(ctx context.Context, session VoiceAgentSession) (int64, error) {
	session = normalizeVoiceAgentSession(session)
	turnsJSON, ideasJSON, decisionsJSON, questionsJSON, stepsJSON, err := marshalVoiceAgentSessionJSON(session)
	if err != nil {
		return 0, err
	}

	result, err := s.db.ExecContext(ctx,
		`INSERT INTO voice_agent_sessions (
			title, summary, raw_summary, transcript, language, provider_profile_id, runtime_kind,
			turns_json, ideas_json, decisions_json, open_questions_json, next_steps_json, started_at, ended_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.Summary.Title,
		session.Summary.Summary,
		session.Summary.RawText,
		session.Transcript,
		session.Language,
		session.ProviderProfileID,
		session.RuntimeKind,
		turnsJSON,
		ideasJSON,
		decisionsJSON,
		questionsJSON,
		stepsJSON,
		session.StartedAt,
		session.EndedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert voice agent session: %w", err)
	}
	return result.LastInsertId()
}

func (s *SQLiteStore) ListVoiceAgentSessions(ctx context.Context, opts ListOpts) ([]VoiceAgentSession, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, summary, raw_summary, transcript, language, provider_profile_id, runtime_kind,
			turns_json, ideas_json, decisions_json, open_questions_json, next_steps_json, started_at, ended_at, created_at
		 FROM voice_agent_sessions
		 ORDER BY created_at DESC, id DESC
		 LIMIT ? OFFSET ?`,
		limit, opts.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	return scanVoiceAgentSessions(rows)
}

func (s *PostgresStore) SaveVoiceAgentSession(ctx context.Context, session VoiceAgentSession) (int64, error) {
	session = normalizeVoiceAgentSession(session)
	turnsJSON, ideasJSON, decisionsJSON, questionsJSON, stepsJSON, err := marshalVoiceAgentSessionJSON(session)
	if err != nil {
		return 0, err
	}

	var id int64
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO voice_agent_sessions (
			title, summary, raw_summary, transcript, language, provider_profile_id, runtime_kind,
			turns_json, ideas_json, decisions_json, open_questions_json, next_steps_json, started_at, ended_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10::jsonb, $11::jsonb, $12::jsonb, $13, $14)
		RETURNING id`,
		session.Summary.Title,
		session.Summary.Summary,
		session.Summary.RawText,
		session.Transcript,
		session.Language,
		session.ProviderProfileID,
		session.RuntimeKind,
		turnsJSON,
		ideasJSON,
		decisionsJSON,
		questionsJSON,
		stepsJSON,
		session.StartedAt,
		session.EndedAt,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert voice agent session: %w", err)
	}
	return id, nil
}

func (s *PostgresStore) ListVoiceAgentSessions(ctx context.Context, opts ListOpts) ([]VoiceAgentSession, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, summary, raw_summary, transcript, language, provider_profile_id, runtime_kind,
			turns_json::text, ideas_json::text, decisions_json::text, open_questions_json::text, next_steps_json::text,
			started_at, ended_at, created_at
		 FROM voice_agent_sessions
		 ORDER BY created_at DESC, id DESC
		 LIMIT $1 OFFSET $2`,
		limit, opts.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	return scanVoiceAgentSessions(rows)
}

type voiceAgentSessionRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
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

func marshalVoiceAgentSessionJSON(session VoiceAgentSession) (turns, ideas, decisions, questions, steps string, err error) {
	turns, err = marshalJSON(session.Turns)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("marshal voice agent turns: %w", err)
	}
	ideas, err = marshalJSON(session.Summary.Ideas)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("marshal voice agent ideas: %w", err)
	}
	decisions, err = marshalJSON(session.Summary.Decisions)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("marshal voice agent decisions: %w", err)
	}
	questions, err = marshalJSON(session.Summary.OpenQuestions)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("marshal voice agent open questions: %w", err)
	}
	steps, err = marshalJSON(session.Summary.NextSteps)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("marshal voice agent next steps: %w", err)
	}
	return turns, ideas, decisions, questions, steps, nil
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
