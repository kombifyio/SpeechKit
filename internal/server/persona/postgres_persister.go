//go:build linux

package persona

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// PostgresPersister implements Persister against the Postgres store schema.
type PostgresPersister struct {
	db *sql.DB
}

// NewPostgresPersister wraps an already-open *sql.DB. The caller owns the
// connection lifecycle; the persister never closes it.
func NewPostgresPersister(db *sql.DB) *PostgresPersister {
	return &PostgresPersister{db: db}
}

const postgresPersonaUpsertSQL = `
INSERT INTO voice_agent_personas
    (id, display_name, description, voice, locale, default_role, default_sequence, tags_json, metadata_json, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11)
ON CONFLICT(id) DO UPDATE SET
    display_name     = excluded.display_name,
    description      = excluded.description,
    voice            = excluded.voice,
    locale           = excluded.locale,
    default_role     = excluded.default_role,
    default_sequence = excluded.default_sequence,
    tags_json        = excluded.tags_json,
    metadata_json    = excluded.metadata_json,
    updated_at       = excluded.updated_at
`

func (p *PostgresPersister) SavePersona(ctx context.Context, entity Persona) error {
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
	_, err = p.db.ExecContext(ctx, postgresPersonaUpsertSQL,
		entity.ID, entity.DisplayName, entity.Description, entity.Voice, entity.Locale,
		entity.DefaultRole, entity.DefaultSequence, string(tagsJSON), string(metaJSON),
		entity.CreatedAt.UTC(), entity.UpdatedAt.UTC(),
	)
	return err
}

func (p *PostgresPersister) DeletePersona(ctx context.Context, id string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM voice_agent_personas WHERE id = $1`, id)
	return err
}

func (p *PostgresPersister) LoadPersonas(ctx context.Context) ([]Persona, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, display_name, description, voice, locale, default_role, default_sequence,
		       tags_json::text, metadata_json::text, created_at, updated_at
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
			&e.DefaultSequence, &tagsJSON, &metaJSON, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsJSON), &e.Tags)
		_ = json.Unmarshal([]byte(metaJSON), &e.Metadata)
		out = append(out, e)
	}
	return out, rows.Err()
}

const postgresRoleUpsertSQL = `
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
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25)
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

func (p *PostgresPersister) SaveRole(ctx context.Context, r Role) error {
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
	_, err = p.db.ExecContext(ctx, postgresRoleUpsertSQL,
		r.ID, r.DisplayName, r.SystemPrompt, r.RefinementPrompt, r.Locale, r.VocabularyHint,
		string(toolsJSON), r.Temperature,
		r.ThinkingEnabled, r.ThinkingLevel, r.IncludeThoughts, r.ThinkingBudget,
		r.AutomaticActivityDetection, r.VADStartSensitivity, r.VADEndSensitivity,
		r.VADPrefixPaddingMs, r.VADSilenceDurationMs,
		r.ActivityHandling, r.TurnCoverage,
		r.ContextCompressionEnabled, r.ContextCompressionTriggerTk, r.ContextCompressionTargetTk,
		r.EnableAffectiveDialog,
		r.CreatedAt.UTC(), r.UpdatedAt.UTC(),
	)
	return err
}

func (p *PostgresPersister) DeleteRole(ctx context.Context, id string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM voice_agent_roles WHERE id = $1`, id)
	return err
}

func (p *PostgresPersister) LoadRoles(ctx context.Context) ([]Role, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, display_name, system_prompt, refinement_prompt, locale, vocabulary_hint,
		       tool_allowlist_json::text, temperature,
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
			r              Role
			toolsJSON      string
			thinkingBudget int64
			prefixPadding  int64
			silenceMs      int64
		)
		if err := rows.Scan(
			&r.ID, &r.DisplayName, &r.SystemPrompt, &r.RefinementPrompt, &r.Locale, &r.VocabularyHint,
			&toolsJSON, &r.Temperature,
			&r.ThinkingEnabled, &r.ThinkingLevel, &r.IncludeThoughts, &thinkingBudget,
			&r.AutomaticActivityDetection, &r.VADStartSensitivity, &r.VADEndSensitivity,
			&prefixPadding, &silenceMs,
			&r.ActivityHandling, &r.TurnCoverage,
			&r.ContextCompressionEnabled, &r.ContextCompressionTriggerTk, &r.ContextCompressionTargetTk,
			&r.EnableAffectiveDialog,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(toolsJSON), &r.ToolAllowlist)
		r.ThinkingBudget = int(thinkingBudget)
		r.VADPrefixPaddingMs = int(prefixPadding)
		r.VADSilenceDurationMs = int(silenceMs)
		out = append(out, r)
	}
	return out, rows.Err()
}

const postgresSequenceUpsertSQL = `
INSERT INTO voice_agent_sequences
    (id, display_name, description, completion, max_turns, steps_json, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8)
ON CONFLICT(id) DO UPDATE SET
    display_name = excluded.display_name,
    description  = excluded.description,
    completion   = excluded.completion,
    max_turns    = excluded.max_turns,
    steps_json   = excluded.steps_json,
    updated_at   = excluded.updated_at
`

func (p *PostgresPersister) SaveSequence(ctx context.Context, s Sequence) error {
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
	_, err = p.db.ExecContext(ctx, postgresSequenceUpsertSQL,
		s.ID, s.DisplayName, s.Description, s.Completion, s.MaxTurns,
		string(stepsJSON), s.CreatedAt.UTC(), s.UpdatedAt.UTC(),
	)
	return err
}

func (p *PostgresPersister) DeleteSequence(ctx context.Context, id string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM voice_agent_sequences WHERE id = $1`, id)
	return err
}

func (p *PostgresPersister) LoadSequences(ctx context.Context) ([]Sequence, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, display_name, description, completion, max_turns, steps_json::text, created_at, updated_at
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
			maxTurns  int64
			stepsJSON string
		)
		if err := rows.Scan(
			&s.ID, &s.DisplayName, &s.Description, &s.Completion, &maxTurns,
			&stepsJSON, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(stepsJSON), &s.Steps)
		s.MaxTurns = int(maxTurns)
		out = append(out, s)
	}
	return out, rows.Err()
}
