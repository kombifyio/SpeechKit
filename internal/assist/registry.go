package assist

import (
	"sort"

	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

type UtilityID string

const (
	UtilityCopyLast   UtilityID = "copy_last"
	UtilityInsertLast UtilityID = "insert_last"
	UtilitySummarize  UtilityID = "summarize"
	UtilityQuickNote  UtilityID = "quick_note"

	// Voice-Companion utilities. Wired in Phase 0 with Enabled=false; the
	// per-skill executors land in internal/assist/skills/voice_companion/
	// during Phase 1 and flip Enabled=true through the host registration
	// path. See docs/voice-companion.md.
	UtilityTime          UtilityID = "time"
	UtilityDate          UtilityID = "date"
	UtilityWeather       UtilityID = "weather"
	UtilityTimer         UtilityID = "timer"
	UtilityReminder      UtilityID = "reminder"
	UtilityMath          UtilityID = "math"
	UtilityWikipedia     UtilityID = "wikipedia"
	UtilityHomeAssistant UtilityID = "home_assistant"
)

type UtilityInputRequirement string

const (
	UtilityInputNone              UtilityInputRequirement = "none"
	UtilityInputLastTranscript    UtilityInputRequirement = "last_transcript"
	UtilityInputSelectionOptional UtilityInputRequirement = "selection_optional"
	UtilityInputUtterance         UtilityInputRequirement = "utterance"
)

type UtilityDefinition struct {
	ID             UtilityID
	Intent         shortcuts.Intent
	Label          string
	Input          UtilityInputRequirement
	DefaultSurface ResultSurface
	DefaultKind    ResultKind
	RequiresModel  bool
	Enabled        bool
}

type UtilityRegistry struct {
	utilities map[shortcuts.Intent]UtilityDefinition
}

func DefaultUtilityRegistry() *UtilityRegistry {
	registry := NewUtilityRegistry()
	registry.Register(UtilityDefinition{
		ID:             UtilityCopyLast,
		Intent:         shortcuts.IntentCopyLast,
		Label:          "Copy last transcription",
		Input:          UtilityInputLastTranscript,
		DefaultSurface: ResultSurfaceActionAck,
		DefaultKind:    ResultKindUtilityAction,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityInsertLast,
		Intent:         shortcuts.IntentInsertLast,
		Label:          "Insert last transcription",
		Input:          UtilityInputLastTranscript,
		DefaultSurface: ResultSurfaceActionAck,
		DefaultKind:    ResultKindUtilityAction,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilitySummarize,
		Intent:         shortcuts.IntentSummarize,
		Label:          "Summarize selection or recent text",
		Input:          UtilityInputSelectionOptional,
		DefaultSurface: ResultSurfacePanel,
		DefaultKind:    ResultKindWorkProduct,
		RequiresModel:  true,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityQuickNote,
		Intent:         shortcuts.IntentQuickNote,
		Label:          "Create quick note",
		Input:          UtilityInputUtterance,
		DefaultSurface: ResultSurfaceActionAck,
		DefaultKind:    ResultKindUtilityAction,
		Enabled:        false,
	})

	// Voice-Companion utilities. Phase 0 wires the catalog with Enabled=false
	// so a host that opts in (via [assist].enabled_tools or via SDK
	// UtilityRegistry.Register override) sees the entries; the per-skill
	// executors land in internal/assist/skills/voice_companion/ during
	// Phase 1. See docs/voice-companion.md.
	registry.Register(UtilityDefinition{
		ID:             UtilityTime,
		Intent:         shortcuts.IntentTime,
		Label:          "Tell the current time",
		Input:          UtilityInputNone,
		DefaultSurface: ResultSurfacePanel,
		DefaultKind:    ResultKindAnswer,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityDate,
		Intent:         shortcuts.IntentDate,
		Label:          "Tell today's date",
		Input:          UtilityInputNone,
		DefaultSurface: ResultSurfacePanel,
		DefaultKind:    ResultKindAnswer,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityWeather,
		Intent:         shortcuts.IntentWeather,
		Label:          "Look up the weather forecast",
		Input:          UtilityInputUtterance,
		DefaultSurface: ResultSurfacePanel,
		DefaultKind:    ResultKindAnswer,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityTimer,
		Intent:         shortcuts.IntentTimer,
		Label:          "Set a countdown timer",
		Input:          UtilityInputUtterance,
		DefaultSurface: ResultSurfaceActionAck,
		DefaultKind:    ResultKindUtilityAction,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityReminder,
		Intent:         shortcuts.IntentReminder,
		Label:          "Set a scheduled reminder",
		Input:          UtilityInputUtterance,
		DefaultSurface: ResultSurfaceActionAck,
		DefaultKind:    ResultKindUtilityAction,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityMath,
		Intent:         shortcuts.IntentMath,
		Label:          "Evaluate a math expression",
		Input:          UtilityInputUtterance,
		DefaultSurface: ResultSurfacePanel,
		DefaultKind:    ResultKindAnswer,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityWikipedia,
		Intent:         shortcuts.IntentWikipedia,
		Label:          "Summarize a topic from Wikipedia",
		Input:          UtilityInputUtterance,
		DefaultSurface: ResultSurfacePanel,
		DefaultKind:    ResultKindAnswer,
		Enabled:        true,
	})
	// HomeAssistant is registered but disabled by default — the host
	// flips it on once a URL+Token are configured. Without that the
	// CompositeExecutor short-circuits the skill to silent anyway, so
	// the Enabled flag prevents the router from claiming an HA match
	// when no integration is wired.
	registry.Register(UtilityDefinition{
		ID:             UtilityHomeAssistant,
		Intent:         shortcuts.IntentHomeAssistant,
		Label:          "Send command to Home Assistant",
		Input:          UtilityInputUtterance,
		DefaultSurface: ResultSurfaceActionAck,
		DefaultKind:    ResultKindUtilityAction,
		Enabled:        false,
	})
	return registry
}

func NewUtilityRegistry() *UtilityRegistry {
	return &UtilityRegistry{utilities: map[shortcuts.Intent]UtilityDefinition{}}
}

func (r *UtilityRegistry) Register(def UtilityDefinition) {
	if r == nil || def.Intent == shortcuts.IntentNone {
		return
	}
	if def.ID == "" {
		def.ID = UtilityID(def.Intent)
	}
	if def.DefaultSurface == "" {
		def.DefaultSurface = ResultSurfaceActionAck
	}
	if def.DefaultKind == "" {
		def.DefaultKind = ResultKindUtilityAction
	}
	r.utilities[def.Intent] = def
}

func (r *UtilityRegistry) Definition(intent shortcuts.Intent) (UtilityDefinition, bool) {
	if r == nil {
		return UtilityDefinition{}, false
	}
	def, ok := r.utilities[intent]
	if !ok || !def.Enabled {
		return UtilityDefinition{}, false
	}
	return def, true
}

func (r *UtilityRegistry) Supports(intent shortcuts.Intent) bool {
	_, ok := r.Definition(intent)
	return ok
}

func (r *UtilityRegistry) List() []UtilityDefinition {
	if r == nil {
		return nil
	}
	defs := make([]UtilityDefinition, 0, len(r.utilities))
	for _, def := range r.utilities {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
