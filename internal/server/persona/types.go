//go:build linux

// Package persona provides the Voice Agent persona / role / sequence catalog
// for the Server-Target. A Persona is a long-lived customer-facing identity
// that binds a voice, default role, and metadata. A Role is a behavioural
// policy (system prompt, thinking, VAD) that can be assigned to any Persona.
// A Sequence is an optional multi-step workflow the Voice Agent follows in a
// given session.
//
// Storage follows the TOML-seed + optional-store-override pattern documented
// in the server plan: deploy/config/server.example.toml carries immutable
// defaults committed to the repo, and DB entries (M5b, pending) override
// by ID at runtime.
package persona

import "time"

// Persona is a long-lived Voice Agent identity bound to a voice and a
// default role. The ID is stable and safe to reference from clients
// (e.g. an integrating app selects a persona_id when creating a Voice Agent
// session).
type Persona struct {
	ID              string            `json:"id"`
	DisplayName     string            `json:"display_name"`
	Description     string            `json:"description,omitempty"`
	Voice           string            `json:"voice,omitempty"`
	Locale          string            `json:"locale,omitempty"`
	DefaultRole     string            `json:"default_role,omitempty"`
	DefaultSequence string            `json:"default_sequence,omitempty"`
	Tags            []string          `json:"tags,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	// Source records where the entry came from ("toml" or "store"). Clients
	// can use this to distinguish deploy-time defaults from admin-authored
	// overrides.
	Source string `json:"source,omitempty"`
}

// Role is a behavioural policy that can be assigned to any Persona. It owns
// the system prompt, thinking policy, and activity-detection defaults — the
// knobs that actually shape live model behaviour.
type Role struct {
	ID                          string    `json:"id"`
	DisplayName                 string    `json:"display_name"`
	SystemPrompt                string    `json:"system_prompt"`
	RefinementPrompt            string    `json:"refinement_prompt,omitempty"`
	Locale                      string    `json:"locale,omitempty"`
	VocabularyHint              string    `json:"vocabulary_hint,omitempty"`
	ToolAllowlist               []string  `json:"tool_allowlist,omitempty"`
	Temperature                 float64   `json:"temperature,omitempty"`
	ThinkingEnabled             bool      `json:"thinking_enabled,omitempty"`
	ThinkingLevel               string    `json:"thinking_level,omitempty"`
	IncludeThoughts             bool      `json:"include_thoughts,omitempty"`
	ThinkingBudget              int       `json:"thinking_budget,omitempty"`
	AutomaticActivityDetection  bool      `json:"automatic_activity_detection,omitempty"`
	VADStartSensitivity         string    `json:"vad_start_sensitivity,omitempty"`
	VADEndSensitivity           string    `json:"vad_end_sensitivity,omitempty"`
	VADPrefixPaddingMs          int       `json:"vad_prefix_padding_ms,omitempty"`
	VADSilenceDurationMs        int       `json:"vad_silence_duration_ms,omitempty"`
	ActivityHandling            string    `json:"activity_handling,omitempty"`
	TurnCoverage                string    `json:"turn_coverage,omitempty"`
	ContextCompressionEnabled   bool      `json:"context_compression_enabled,omitempty"`
	ContextCompressionTriggerTk int64     `json:"context_compression_trigger_tokens,omitempty"`
	ContextCompressionTargetTk  int64     `json:"context_compression_target_tokens,omitempty"`
	EnableAffectiveDialog       bool      `json:"enable_affective_dialog,omitempty"`
	CreatedAt                   time.Time `json:"created_at"`
	UpdatedAt                   time.Time `json:"updated_at"`
	Source                      string    `json:"source,omitempty"`
}

// Sequence is an optional multi-step workflow the Voice Agent follows in a
// session. Steps are ordered; advancing between them is driven by explicit
// client frames in v1 (M4) and by LLM-judged exit criteria in v1.1 (M5b+).
type Sequence struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Description string `json:"description,omitempty"`
	// Completion strategy: "all_steps" | "explicit_close" | "max_turns".
	Completion string         `json:"completion,omitempty"`
	MaxTurns   int            `json:"max_turns,omitempty"`
	Steps      []SequenceStep `json:"steps"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	Source     string         `json:"source,omitempty"`
}

// SequenceStep is a single step in a Sequence.
type SequenceStep struct {
	ID           string   `json:"id"`
	Instruction  string   `json:"instruction"`
	ExitCriteria string   `json:"exit_criteria,omitempty"`
	RequireTools []string `json:"require_tools,omitempty"`
	MaxTurns     int      `json:"max_turns,omitempty"`
}

// ResolvedSession captures the fully composed config the Voice Agent
// WebSocket adapter hands to the live provider. It mirrors
// internal/server/voiceagent.LiveConfigFrame but stays in this package so
// the persona layer has no dependency on the voiceagent adapter.
type ResolvedSession struct {
	PersonaID          string
	RoleID             string
	SequenceID         string
	SequenceCompletion string
	SequenceMaxTurns   int
	Voice              string
	Locale             string
	Model              string
	SystemPrompt       string
	RefinementPrompt   string
	Thinking           string
	AutomaticVAD       bool
	StartSensitivity   string
	EndSensitivity     string
	PrefixPaddingMs    int32
	SilenceDurationMs  int32
	ActivityHandling   string
	TurnCoverage       string
	CurrentStep        *SequenceStep
	StepID             string
	StepIndex          int
	StepCount          int
	StepInstruction    string
	StepExitCriteria   string
	StepMaxTurns       int
}
