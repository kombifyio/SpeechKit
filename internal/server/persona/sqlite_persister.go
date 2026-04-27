//go:build linux

package persona

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SQLitePersister implements Persister against a *sql.DB whose schema was
// created by migration 007_personas.sql. Works for both the Framework's
// SQLite backend (modernc.org/sqlite) and, by extension, any SQL driver
// that speaks the same dialect (SQLite file paths, :memory:, etc.).
//
// Postgres support requires a sibling file with pg-dialect placeholders
// ($1,$2,...) and would replace `ON CONFLICT(id) DO UPDATE SET ...` with
// the same shape — the schema in 007_personas.sql is already Postgres-
// compatible.
type SQLitePersister struct {
	db *sql.DB
}

// NewSQLitePersister wraps an already-open *sql.DB. The caller owns the
// connection lifecycle; the persister never closes it.
func NewSQLitePersister(db *sql.DB) *SQLitePersister {
	return &SQLitePersister{db: db}
}

// ── Personas ────────────────────────────────────────────────────────────────

const personaUpsertSQL = `
INSERT INTO voice_agent_personas
    (id, display_name, description, voice, locale, default_role, tags_json, metadata_json, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    display_name  = excluded.display_name,
    description   = excluded.description,
    voice         = excluded.voice,
    locale        = excluded.locale,
    default_role  = excluded.default_role,
    tags_json     = excluded.tags_json,
    metadata_json = excluded.metadata_json,
    updated_at    = excluded.updated_at
`

func (p *SQLitePersister) SavePersona(ctx context.Context, entity Persona) error {
	tagsJSON, err := json.Marshal(entity.Tags)
	if err != nil {
		return fmt.Errorf("persona: marshal tags: %w", err)
	}
	metaJSON, err := json.Marshal(entity.Metadata)
	if err != nil {
		return fmt.Errorf("persona: marshal metadata: %w", err)
	}
	if entity.CreatedAt.IsZero() {
		entity.CreatedAt = time.Now().UTC()
	}
	if entity.UpdatedAt.IsZero() {
		entity.UpdatedAt = entity.CreatedAt
	}
	_, err = p.db.ExecContext(ctx, personaUpsertSQL,
		entity.ID, entity.DisplayName, entity.Description, entity.Voice, entity.Locale,
		entity.DefaultRole, string(tagsJSON), string(metaJSON),
		entity.CreatedAt.UTC(), entity.UpdatedAt.UTC(),
	)
	return err
}

func (p *SQLitePersister) DeletePersona(ctx context.Context, id string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM voice_agent_personas WHERE id = ?`, id)
	return err
}

func (p *SQLitePersister) LoadPersonas(ctx context.Context) ([]Persona, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, display_name, description, voice, locale, default_role,
		       tags_json, metadata_json, created_at, updated_at
		FROM voice_agent_personas
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var out []Persona
	for rows.Next() {
		var (
			e                  Persona
			tagsJSON, metaJSON string
		)
		if err := rows.Scan(
			&e.ID, &e.DisplayName, &e.Description, &e.Voice, &e.Locale, &e.DefaultRole,
			&tagsJSON, &metaJSON, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsJSON), &e.Tags)
		_ = json.Unmarshal([]byte(metaJSON), &e.Metadata)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ── Roles ───────────────────────────────────────────────────────────────────

const roleUpsertSQL = `
INSERT INTO voice_agent_roles
    (id, display_name, system_prompt, refinement_prompt, locale, vocabulary_hint,
     tool_allowlist_json, temperature,
     thinking_enabled, thinking_level, include_thoughts, thinking_budget,
     automatic_activity_detection, vad_start_sensitivity, vad_end_sensitivity,
     vad_prefix_padding_ms, vad_silence_duration_ms,
     activity_handling, turn_coverage,
     context_compression_enabled, context_compression_trigger_tk, context_compression_target_tk,
     enable_affective_dialog,
     created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    display_name                   = excluded.display_name,
    system_prompt                  = excluded.system_prompt,
    refinement_prompt              = excluded.refinement_prompt,
    locale                         = excluded.locale,
    vocabulary_hint                = excluded.vocabulary_hint,
    tool_allowlist_json            = excluded.tool_allowlist_json,
    temperature                    = excluded.temperature,
    thinking_enabled               = excluded.thinking_enabled,
    thinking_level                 = excluded.thinking_level,
    include_thoughts               = excluded.include_thoughts,
    thinking_budget                = excluded.thinking_budget,
    automatic_activity_detection   = excluded.automatic_activity_detection,
    vad_start_sensitivity          = excluded.vad_start_sensitivity,
    vad_end_sensitivity            = excluded.vad_end_sensitivity,
    vad_prefix_padding_ms          = excluded.vad_prefix_padding_ms,
    vad_silence_duration_ms        = excluded.vad_silence_duration_ms,
    activity_handling              = excluded.activity_handling,
    turn_coverage                  = excluded.turn_coverage,
    context_compression_enabled    = excluded.context_compression_enabled,
    context_compression_trigger_tk = excluded.context_compression_trigger_tk,
    context_compression_target_tk  = excluded.context_compression_target_tk,
    enable_affective_dialog        = excluded.enable_affective_dialog,
    updated_at                     = excluded.updated_at
`

func (p *SQLitePersister) SaveRole(ctx context.Context, r Role) error {
	toolsJSON, err := json.Marshal(r.ToolAllowlist)
	if err != nil {
		return fmt.Errorf("persona: marshal tool allowlist: %w", err)
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = r.CreatedAt
	}
	_, err = p.db.ExecContext(ctx, roleUpsertSQL,
		r.ID, r.DisplayName, r.SystemPrompt, r.RefinementPrompt, r.Locale, r.VocabularyHint,
		string(toolsJSON), r.Temperature,
		boolToInt(r.ThinkingEnabled), r.ThinkingLevel, boolToInt(r.IncludeThoughts), r.ThinkingBudget,
		boolToInt(r.AutomaticActivityDetection), r.VADStartSensitivity, r.VADEndSensitivity,
		r.VADPrefixPaddingMs, r.VADSilenceDurationMs,
		r.ActivityHandling, r.TurnCoverage,
		boolToInt(r.ContextCompressionEnabled), r.ContextCompressionTriggerTk, r.ContextCompressionTargetTk,
		boolToInt(r.EnableAffectiveDialog),
		r.CreatedAt.UTC(), r.UpdatedAt.UTC(),
	)
	return err
}

func (p *SQLitePersister) DeleteRole(ctx context.Context, id string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM voice_agent_roles WHERE id = ?`, id)
	return err
}

func (p *SQLitePersister) LoadRoles(ctx context.Context) ([]Role, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, display_name, system_prompt, refinement_prompt, locale, vocabulary_hint,
		       tool_allowlist_json, temperature,
		       thinking_enabled, thinking_level, include_thoughts, thinking_budget,
		       automatic_activity_detection, vad_start_sensitivity, vad_end_sensitivity,
		       vad_prefix_padding_ms, vad_silence_duration_ms,
		       activity_handling, turn_coverage,
		       context_compression_enabled, context_compression_trigger_tk, context_compression_target_tk,
		       enable_affective_dialog,
		       created_at, updated_at
		FROM voice_agent_roles
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var out []Role
	for rows.Next() {
		var (
			r                                                                         Role
			toolsJSON                                                                 string
			thinkingEnabled, includeThoughts, autoActivity, ctxCompression, affective int
		)
		if err := rows.Scan(
			&r.ID, &r.DisplayName, &r.SystemPrompt, &r.RefinementPrompt, &r.Locale, &r.VocabularyHint,
			&toolsJSON, &r.Temperature,
			&thinkingEnabled, &r.ThinkingLevel, &includeThoughts, &r.ThinkingBudget,
			&autoActivity, &r.VADStartSensitivity, &r.VADEndSensitivity,
			&r.VADPrefixPaddingMs, &r.VADSilenceDurationMs,
			&r.ActivityHandling, &r.TurnCoverage,
			&ctxCompression, &r.ContextCompressionTriggerTk, &r.ContextCompressionTargetTk,
			&affective,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(toolsJSON), &r.ToolAllowlist)
		r.ThinkingEnabled = thinkingEnabled != 0
		r.IncludeThoughts = includeThoughts != 0
		r.AutomaticActivityDetection = autoActivity != 0
		r.ContextCompressionEnabled = ctxCompression != 0
		r.EnableAffectiveDialog = affective != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// ── Sequences ───────────────────────────────────────────────────────────────

const sequenceUpsertSQL = `
INSERT INTO voice_agent_sequences
    (id, display_name, description, completion, max_turns, steps_json, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    display_name = excluded.display_name,
    description  = excluded.description,
    completion   = excluded.completion,
    max_turns    = excluded.max_turns,
    steps_json   = excluded.steps_json,
    updated_at   = excluded.updated_at
`

func (p *SQLitePersister) SaveSequence(ctx context.Context, s Sequence) error {
	stepsJSON, err := json.Marshal(s.Steps)
	if err != nil {
		return fmt.Errorf("persona: marshal steps: %w", err)
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = s.CreatedAt
	}
	_, err = p.db.ExecContext(ctx, sequenceUpsertSQL,
		s.ID, s.DisplayName, s.Description, s.Completion, s.MaxTurns,
		string(stepsJSON), s.CreatedAt.UTC(), s.UpdatedAt.UTC(),
	)
	return err
}

func (p *SQLitePersister) DeleteSequence(ctx context.Context, id string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM voice_agent_sequences WHERE id = ?`, id)
	return err
}

func (p *SQLitePersister) LoadSequences(ctx context.Context) ([]Sequence, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, display_name, description, completion, max_turns, steps_json, created_at, updated_at
		FROM voice_agent_sequences
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var out []Sequence
	for rows.Next() {
		var (
			s         Sequence
			stepsJSON string
		)
		if err := rows.Scan(
			&s.ID, &s.DisplayName, &s.Description, &s.Completion, &s.MaxTurns,
			&stepsJSON, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(stepsJSON), &s.Steps)
		out = append(out, s)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
