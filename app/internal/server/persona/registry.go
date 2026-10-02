//go:build linux

package persona

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// Errors returned by the registry. Exposed so handlers can map to HTTP
// statuses cleanly.
var (
	ErrPersonaNotFound  = errors.New("persona: persona not found")
	ErrRoleNotFound     = errors.New("persona: role not found")
	ErrSequenceNotFound = errors.New("persona: sequence not found")
	ErrAlreadyExists    = errors.New("persona: entity with this id already exists")
	ErrStepNotFound     = errors.New("persona: sequence step not found")
	// ErrPersist wraps any error returned by the durable Persister so
	// callers can distinguish validation failures (400) from
	// persistence failures (500). Use errors.Is(err, ErrPersist).
	ErrPersist = errors.New("persona: persist failed")
)

// Registry is the in-memory catalog of personas, roles, and sequences. It is
// safe for concurrent reads + writes (RWMutex). When a Persister is
// attached via WithPersister, admin-authored writes round-trip through the
// persistence layer (M5b); TOML seeds remain memory-only.
type Registry struct {
	mu        sync.RWMutex
	personas  map[string]*Persona
	roles     map[string]*Role
	sequences map[string]*Sequence
	clock     func() time.Time
	persister Persister
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		personas:  map[string]*Persona{},
		roles:     map[string]*Role{},
		sequences: map[string]*Sequence{},
		clock:     time.Now,
	}
}

// WithClock overrides the time source used for CreatedAt/UpdatedAt. Tests
// use a mutable clock so they can assert exact timestamps.
func (r *Registry) WithClock(fn func() time.Time) *Registry {
	if fn != nil {
		r.clock = fn
	}
	return r
}

// ── Personas ────────────────────────────────────────────────────────────────

// UpsertPersona inserts or replaces a persona. Returns a copy so callers
// can't mutate internal state. When a Persister is attached, the change is
// persisted FIRST and only committed to memory after success — a persist
// error means the in-memory state is unchanged and the caller sees a
// concrete error. Source="toml" entries skip the persister entirely so
// repo-committed seeds never round-trip to the store.
func (r *Registry) UpsertPersona(p Persona) (Persona, error) {
	if err := validatePersona(p); err != nil {
		return Persona{}, err
	}

	// Compute final values without mutating the registry yet — we need
	// CreatedAt + UpdatedAt to be persisted alongside the row, so they
	// have to be set before the persister sees the entity.
	r.mu.Lock()
	now := r.clock().UTC()
	if existing, ok := r.personas[p.ID]; ok {
		p.CreatedAt = existing.CreatedAt
	} else {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if p.Source == "" {
		p.Source = "store"
	}
	persister := r.persister
	r.mu.Unlock()

	if persister != nil && p.Source == "store" {
		if err := persister.SavePersona(context.Background(), p); err != nil {
			slog.Warn("persona: persister.SavePersona failed; refusing to commit in-memory mutation",
				"id", p.ID, "err", err)
			return Persona{}, fmt.Errorf("%w: SavePersona: %w", ErrPersist, err)
		}
	}

	// Persistence (or no persister) succeeded — commit to memory.
	r.mu.Lock()
	copied := p
	r.personas[p.ID] = &copied
	r.mu.Unlock()
	return clonePersona(copied), nil
}

// GetPersona returns a copy by ID.
func (r *Registry) GetPersona(id string) (Persona, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.personas[id]
	if !ok {
		return Persona{}, ErrPersonaNotFound
	}
	return clonePersona(*p), nil
}

// ListPersonas returns copies sorted by ID. Empty slice when the registry is
// empty — never nil.
func (r *Registry) ListPersonas() []Persona {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Persona, 0, len(r.personas))
	for _, p := range r.personas {
		out = append(out, clonePersona(*p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// DeletePersona removes the entry. Returns ErrPersonaNotFound when missing,
// or a persistence error when the durable delete fails (the in-memory entry
// stays in place in that case so the registry is consistent with storage).
func (r *Registry) DeletePersona(id string) error {
	r.mu.RLock()
	_, ok := r.personas[id]
	persister := r.persister
	r.mu.RUnlock()
	if !ok {
		return ErrPersonaNotFound
	}
	if persister != nil {
		if err := persister.DeletePersona(context.Background(), id); err != nil {
			slog.Warn("persona: persister.DeletePersona failed; in-memory entry kept for consistency", // #nosec G706 -- slog writes persisted IDs/errors as structured attributes, not interpolated log text.
				"id", id, "err", err)
			return fmt.Errorf("%w: DeletePersona: %w", ErrPersist, err)
		}
	}
	r.mu.Lock()
	delete(r.personas, id)
	r.mu.Unlock()
	return nil
}

// ── Roles ───────────────────────────────────────────────────────────────────

func (r *Registry) UpsertRole(role Role) (Role, error) {
	if err := validateRole(role); err != nil {
		return Role{}, err
	}
	r.mu.Lock()
	now := r.clock().UTC()
	if existing, ok := r.roles[role.ID]; ok {
		role.CreatedAt = existing.CreatedAt
	} else {
		role.CreatedAt = now
	}
	role.UpdatedAt = now
	if role.Source == "" {
		role.Source = "store"
	}
	persister := r.persister
	r.mu.Unlock()

	if persister != nil && role.Source == "store" {
		if err := persister.SaveRole(context.Background(), role); err != nil {
			slog.Warn("persona: persister.SaveRole failed; refusing to commit in-memory mutation",
				"id", role.ID, "err", err)
			return Role{}, fmt.Errorf("%w: SaveRole: %w", ErrPersist, err)
		}
	}

	r.mu.Lock()
	copied := role
	r.roles[role.ID] = &copied
	r.mu.Unlock()
	return cloneRole(copied), nil
}

func (r *Registry) GetRole(id string) (Role, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	role, ok := r.roles[id]
	if !ok {
		return Role{}, ErrRoleNotFound
	}
	return cloneRole(*role), nil
}

func (r *Registry) ListRoles() []Role {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Role, 0, len(r.roles))
	for _, v := range r.roles {
		out = append(out, cloneRole(*v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *Registry) DeleteRole(id string) error {
	r.mu.RLock()
	_, ok := r.roles[id]
	persister := r.persister
	r.mu.RUnlock()
	if !ok {
		return ErrRoleNotFound
	}
	if persister != nil {
		if err := persister.DeleteRole(context.Background(), id); err != nil {
			slog.Warn("persona: persister.DeleteRole failed; in-memory entry kept for consistency", // #nosec G706 -- slog writes persisted IDs/errors as structured attributes, not interpolated log text.
				"id", id, "err", err)
			return fmt.Errorf("%w: DeleteRole: %w", ErrPersist, err)
		}
	}
	r.mu.Lock()
	delete(r.roles, id)
	r.mu.Unlock()
	return nil
}

// ── Sequences ───────────────────────────────────────────────────────────────

func (r *Registry) UpsertSequence(seq Sequence) (Sequence, error) {
	if err := validateSequence(seq); err != nil {
		return Sequence{}, err
	}
	r.mu.Lock()
	now := r.clock().UTC()
	if existing, ok := r.sequences[seq.ID]; ok {
		seq.CreatedAt = existing.CreatedAt
	} else {
		seq.CreatedAt = now
	}
	seq.UpdatedAt = now
	if seq.Source == "" {
		seq.Source = "store"
	}
	persister := r.persister
	r.mu.Unlock()

	if persister != nil && seq.Source == "store" {
		if err := persister.SaveSequence(context.Background(), seq); err != nil {
			slog.Warn("persona: persister.SaveSequence failed; refusing to commit in-memory mutation",
				"id", seq.ID, "err", err)
			return Sequence{}, fmt.Errorf("%w: SaveSequence: %w", ErrPersist, err)
		}
	}

	r.mu.Lock()
	copied := seq
	r.sequences[seq.ID] = &copied
	r.mu.Unlock()
	return cloneSequence(copied), nil
}

func (r *Registry) GetSequence(id string) (Sequence, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sequences[id]
	if !ok {
		return Sequence{}, ErrSequenceNotFound
	}
	return cloneSequence(*s), nil
}

func (r *Registry) ListSequences() []Sequence {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Sequence, 0, len(r.sequences))
	for _, v := range r.sequences {
		out = append(out, cloneSequence(*v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *Registry) DeleteSequence(id string) error {
	r.mu.RLock()
	_, ok := r.sequences[id]
	persister := r.persister
	r.mu.RUnlock()
	if !ok {
		return ErrSequenceNotFound
	}
	if persister != nil {
		if err := persister.DeleteSequence(context.Background(), id); err != nil {
			slog.Warn("persona: persister.DeleteSequence failed; in-memory entry kept for consistency", // #nosec G706 -- slog writes persisted IDs/errors as structured attributes, not interpolated log text.
				"id", id, "err", err)
			return fmt.Errorf("%w: DeleteSequence: %w", ErrPersist, err)
		}
	}
	r.mu.Lock()
	delete(r.sequences, id)
	r.mu.Unlock()
	return nil
}

// ── Resolution ──────────────────────────────────────────────────────────────

// Resolve composes a ResolvedSession from the requested persona / role /
// sequence IDs. Any empty ID falls back to the persona's default role and a
// nil sequence respectively. Missing IDs return a typed error so the
// WebSocket adapter can surface a clear error code to clients.
//
// The caller (voiceagent adapter) supplies `stepIndex` — which step of the
// sequence to project into CurrentStep. 0 means the first step.
func (r *Registry) Resolve(personaID, roleID, sequenceID string, stepIndex int) (ResolvedSession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	persona, ok := r.personas[strings.TrimSpace(personaID)]
	if !ok {
		return ResolvedSession{}, ErrPersonaNotFound
	}

	effectiveRoleID := strings.TrimSpace(roleID)
	if effectiveRoleID == "" {
		effectiveRoleID = persona.DefaultRole
	}
	var role *Role
	if effectiveRoleID != "" {
		role, ok = r.roles[effectiveRoleID]
		if !ok {
			return ResolvedSession{}, fmt.Errorf("%w: %q", ErrRoleNotFound, effectiveRoleID)
		}
	}

	var seq *Sequence
	var currentStep *SequenceStep
	effectiveSequenceID := strings.TrimSpace(sequenceID)
	if effectiveSequenceID == "" {
		effectiveSequenceID = strings.TrimSpace(persona.DefaultSequence)
	}
	if effectiveSequenceID != "" {
		seq, ok = r.sequences[effectiveSequenceID]
		if !ok {
			return ResolvedSession{}, fmt.Errorf("%w: %q", ErrSequenceNotFound, effectiveSequenceID)
		}
		if stepIndex < 0 || stepIndex >= len(seq.Steps) {
			return ResolvedSession{}, fmt.Errorf("%w: index %d out of range for sequence %q (%d steps)", ErrStepNotFound, stepIndex, sequenceID, len(seq.Steps))
		}
		if len(seq.Steps) > 0 {
			step := seq.Steps[stepIndex]
			currentStep = &step
		}
	}

	result := ResolvedSession{
		PersonaID:   persona.ID,
		RoleID:      effectiveRoleID,
		Voice:       persona.Voice,
		Locale:      persona.Locale,
		CurrentStep: currentStep,
	}
	if seq != nil {
		result.SequenceID = seq.ID
		result.SequenceCompletion = seq.Completion
		result.SequenceMaxTurns = seq.MaxTurns
		result.StepIndex = stepIndex
		result.StepCount = len(seq.Steps)
		if currentStep != nil {
			result.StepID = currentStep.ID
			result.StepInstruction = currentStep.Instruction
			result.StepExitCriteria = currentStep.ExitCriteria
			result.StepMaxTurns = currentStep.MaxTurns
		}
	}

	if role != nil {
		result.SystemPrompt = combinePrompts(role.SystemPrompt, currentStep)
		result.RefinementPrompt = role.RefinementPrompt
		if strings.TrimSpace(role.Locale) != "" {
			result.Locale = role.Locale
		}
		result.Thinking = role.ThinkingLevel
		result.AutomaticVAD = role.AutomaticActivityDetection
		result.StartSensitivity = role.VADStartSensitivity
		result.EndSensitivity = role.VADEndSensitivity
		result.PrefixPaddingMs = intToInt32Clamp(role.VADPrefixPaddingMs)
		result.SilenceDurationMs = intToInt32Clamp(role.VADSilenceDurationMs)
		result.ActivityHandling = role.ActivityHandling
		result.TurnCoverage = role.TurnCoverage
	}
	return result, nil
}

func intToInt32Clamp(value int) int32 {
	const (
		maxInt32 = 1<<31 - 1
		minInt32 = -1 << 31
	)
	switch {
	case value > maxInt32:
		return maxInt32
	case value < minInt32:
		return minInt32
	default:
		return int32(value) // #nosec G115 -- value is clamped to the int32 range above.
	}
}

// combinePrompts merges the role's system prompt with the current sequence
// step instruction. When no step is active the role prompt is returned as-is.
func combinePrompts(rolePrompt string, step *SequenceStep) string {
	rolePrompt = strings.TrimSpace(rolePrompt)
	if step == nil || strings.TrimSpace(step.Instruction) == "" {
		return rolePrompt
	}
	if rolePrompt == "" {
		return step.Instruction
	}
	return rolePrompt + "\n\n[Current step: " + step.ID + "]\n" + step.Instruction
}

// ── helpers ─────────────────────────────────────────────────────────────────

func validatePersona(p Persona) error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("persona: id is required")
	}
	if strings.TrimSpace(p.DisplayName) == "" {
		return errors.New("persona: display_name is required")
	}
	return nil
}

func validateRole(r Role) error {
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("persona: role id is required")
	}
	if strings.TrimSpace(r.DisplayName) == "" {
		return errors.New("persona: role display_name is required")
	}
	if strings.TrimSpace(r.SystemPrompt) == "" {
		return errors.New("persona: role system_prompt is required")
	}
	return nil
}

func validateSequence(s Sequence) error {
	if strings.TrimSpace(s.ID) == "" {
		return errors.New("persona: sequence id is required")
	}
	if strings.TrimSpace(s.DisplayName) == "" {
		return errors.New("persona: sequence display_name is required")
	}
	if len(s.Steps) == 0 {
		return errors.New("persona: sequence must contain at least one step")
	}
	for i, step := range s.Steps {
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("persona: sequence step[%d] id is required", i)
		}
		if strings.TrimSpace(step.Instruction) == "" {
			return fmt.Errorf("persona: sequence step[%d] instruction is required", i)
		}
	}
	return nil
}

func clonePersona(p Persona) Persona {
	c := p
	if len(p.Tags) > 0 {
		c.Tags = append([]string(nil), p.Tags...)
	}
	if len(p.Metadata) > 0 {
		c.Metadata = make(map[string]string, len(p.Metadata))
		for k, v := range p.Metadata {
			c.Metadata[k] = v
		}
	}
	return c
}

func cloneRole(r Role) Role {
	c := r
	if len(r.ToolAllowlist) > 0 {
		c.ToolAllowlist = append([]string(nil), r.ToolAllowlist...)
	}
	return c
}

func cloneSequence(s Sequence) Sequence {
	c := s
	if len(s.Steps) > 0 {
		c.Steps = make([]SequenceStep, len(s.Steps))
		for i, step := range s.Steps {
			stepCopy := step
			if len(step.RequireTools) > 0 {
				stepCopy.RequireTools = append([]string(nil), step.RequireTools...)
			}
			c.Steps[i] = stepCopy
		}
	}
	return c
}
