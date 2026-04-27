//go:build linux

package persona

import (
	"log/slog"

	"github.com/kombifyio/SpeechKit/internal/config"
)

// LoadSeeds populates the registry from the TOML [[personas]], [[roles]],
// and [[sequences]] arrays in the server config. Seeds are always tagged
// Source="toml" so admin overrides (M5b, via store writes) can be
// distinguished via the Source field.
//
// Returns human-readable notes suitable for startup logging. Errors are
// reported per-entry; one invalid seed never blocks the others.
func LoadSeeds(reg *Registry, cfg *config.Config) []string {
	if reg == nil || cfg == nil {
		return nil
	}
	var notes []string

	for _, raw := range cfg.Personas {
		p := Persona{
			ID:          raw.ID,
			DisplayName: raw.DisplayName,
			Description: raw.Description,
			Voice:       raw.Voice,
			Locale:      raw.Locale,
			DefaultRole: raw.DefaultRole,
			Tags:        append([]string(nil), raw.Tags...),
			Metadata:    cloneStringMap(raw.Metadata),
			Source:      "toml",
		}
		if _, err := reg.UpsertPersona(p); err != nil {
			slog.Warn("persona: seed invalid", "id", raw.ID, "err", err)
			notes = append(notes, "persona seed skipped: "+raw.ID+" ("+err.Error()+")")
			continue
		}
		notes = append(notes, "persona seeded: "+raw.ID)
	}

	for _, raw := range cfg.Roles {
		r := Role{
			ID:                          raw.ID,
			DisplayName:                 raw.DisplayName,
			SystemPrompt:                raw.SystemPrompt,
			RefinementPrompt:            raw.RefinementPrompt,
			Locale:                      raw.Locale,
			VocabularyHint:              raw.VocabularyHint,
			ToolAllowlist:               append([]string(nil), raw.ToolAllowlist...),
			Temperature:                 raw.Temperature,
			ThinkingEnabled:             raw.ThinkingEnabled,
			ThinkingLevel:               raw.ThinkingLevel,
			IncludeThoughts:             raw.IncludeThoughts,
			ThinkingBudget:              raw.ThinkingBudget,
			AutomaticActivityDetection:  raw.AutomaticActivityDetection,
			VADStartSensitivity:         raw.VADStartSensitivity,
			VADEndSensitivity:           raw.VADEndSensitivity,
			VADPrefixPaddingMs:          raw.VADPrefixPaddingMs,
			VADSilenceDurationMs:        raw.VADSilenceDurationMs,
			ActivityHandling:            raw.ActivityHandling,
			TurnCoverage:                raw.TurnCoverage,
			ContextCompressionEnabled:   raw.ContextCompressionEnabled,
			ContextCompressionTriggerTk: raw.ContextCompressionTriggerTk,
			ContextCompressionTargetTk:  raw.ContextCompressionTargetTk,
			EnableAffectiveDialog:       raw.EnableAffectiveDialog,
			Source:                      "toml",
		}
		if _, err := reg.UpsertRole(r); err != nil {
			slog.Warn("persona: role seed invalid", "id", raw.ID, "err", err)
			notes = append(notes, "role seed skipped: "+raw.ID+" ("+err.Error()+")")
			continue
		}
		notes = append(notes, "role seeded: "+raw.ID)
	}

	for _, raw := range cfg.Sequences {
		steps := make([]SequenceStep, 0, len(raw.Steps))
		for _, rs := range raw.Steps {
			steps = append(steps, SequenceStep{
				ID:           rs.ID,
				Instruction:  rs.Instruction,
				ExitCriteria: rs.ExitCriteria,
				RequireTools: append([]string(nil), rs.RequireTools...),
				MaxTurns:     rs.MaxTurns,
			})
		}
		s := Sequence{
			ID:          raw.ID,
			DisplayName: raw.DisplayName,
			Description: raw.Description,
			Completion:  raw.Completion,
			MaxTurns:    raw.MaxTurns,
			Steps:       steps,
			Source:      "toml",
		}
		if _, err := reg.UpsertSequence(s); err != nil {
			slog.Warn("persona: sequence seed invalid", "id", raw.ID, "err", err)
			notes = append(notes, "sequence seed skipped: "+raw.ID+" ("+err.Error()+")")
			continue
		}
		notes = append(notes, "sequence seeded: "+raw.ID)
	}

	return notes
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
