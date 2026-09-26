package skills

import (
	"sort"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist/shortcuts"
)

// UtilityID is the stable, host-facing id of a deterministic Assist utility.
// It is what a host lists in its enabled-tools configuration and what a
// settings catalog reports; it equals the intent string for every built-in
// utility.
type UtilityID string

// Built-in utility ids.
const (
	UtilityCopyLast   UtilityID = "copy_last"
	UtilityInsertLast UtilityID = "insert_last"
	UtilitySummarize  UtilityID = "summarize"
	UtilityQuickNote  UtilityID = "quick_note"

	// Voice-Companion utilities, answered by the skills in the companion
	// subpackage. See docs/voice-companion.md.
	UtilityTime          UtilityID = "time"
	UtilityDate          UtilityID = "date"
	UtilityWeather       UtilityID = "weather"
	UtilityTimer         UtilityID = "timer"
	UtilityReminder      UtilityID = "reminder"
	UtilityMath          UtilityID = "math"
	UtilityWikipedia     UtilityID = "wikipedia"
	UtilityTemperature   UtilityID = "temperature"
	UtilityHomeAssistant UtilityID = "home_assistant"
)

// UtilityInputRequirement describes which input a utility consumes; hosts
// use it to decide what to capture before executing.
type UtilityInputRequirement string

// Utility input requirements.
const (
	// UtilityInputNone needs nothing beyond the trigger phrase.
	UtilityInputNone UtilityInputRequirement = "none"
	// UtilityInputLastTranscript operates on the last dictation result.
	UtilityInputLastTranscript UtilityInputRequirement = "last_transcript"
	// UtilityInputSelectionOptional prefers the current selection and falls
	// back to recent text.
	UtilityInputSelectionOptional UtilityInputRequirement = "selection_optional"
	// UtilityInputUtterance consumes the payload spoken after the phrase.
	UtilityInputUtterance UtilityInputRequirement = "utterance"
)

// UtilityDefinition declares one deterministic utility: how it is
// identified, which intent triggers it, what input it needs, which
// presentation defaults apply when the executor leaves Surface or Kind
// empty, and whether it is enabled for routing.
type UtilityDefinition struct {
	ID             UtilityID
	Intent         shortcuts.Intent
	Label          string
	Input          UtilityInputRequirement
	DefaultSurface speechkit.AssistSurfaceDecision
	DefaultKind    string
	// RequiresModel marks utilities that need the Generator to complete
	// (summarize); Catalog.CanHandleLocally reports false for them.
	RequiresModel bool
	// Enabled controls routing: a disabled utility is listed but never
	// matched.
	Enabled bool
}

// UtilityRegistry holds the utilities a Catalog may route to, keyed by
// intent. It is not safe for concurrent mutation; build it before wiring the
// catalog.
type UtilityRegistry struct {
	utilities map[shortcuts.Intent]UtilityDefinition
}

// NewUtilityRegistry returns an empty registry.
func NewUtilityRegistry() *UtilityRegistry {
	return &UtilityRegistry{utilities: map[shortcuts.Intent]UtilityDefinition{}}
}

// DefaultUtilityRegistry returns a fresh registry with every built-in
// utility. Quick note and Home Assistant are registered disabled: quick note
// until a host owns a create-note path, Home Assistant because a configured
// bridge is required. New builds a Catalog from this registry unless
// Options.Registry overrides it, and always enables the Home Assistant entry
// so smart-home commands stay claimed and fail closed.
func DefaultUtilityRegistry() *UtilityRegistry {
	registry := NewUtilityRegistry()
	registry.Register(UtilityDefinition{
		ID:             UtilityCopyLast,
		Intent:         shortcuts.IntentCopyLast,
		Label:          "Copy last transcription",
		Input:          UtilityInputLastTranscript,
		DefaultSurface: speechkit.AssistSurfaceActionAck,
		DefaultKind:    assist.KindUtilityAction,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityInsertLast,
		Intent:         shortcuts.IntentInsertLast,
		Label:          "Insert last transcription",
		Input:          UtilityInputLastTranscript,
		DefaultSurface: speechkit.AssistSurfaceActionAck,
		DefaultKind:    assist.KindUtilityAction,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilitySummarize,
		Intent:         shortcuts.IntentSummarize,
		Label:          "Summarize selection or recent text",
		Input:          UtilityInputSelectionOptional,
		DefaultSurface: speechkit.AssistSurfacePanel,
		DefaultKind:    assist.KindWorkProduct,
		RequiresModel:  true,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityQuickNote,
		Intent:         shortcuts.IntentQuickNote,
		Label:          "Create quick note",
		Input:          UtilityInputUtterance,
		DefaultSurface: speechkit.AssistSurfaceActionAck,
		DefaultKind:    assist.KindUtilityAction,
		Enabled:        false,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityTime,
		Intent:         shortcuts.IntentTime,
		Label:          "Tell the current time",
		Input:          UtilityInputNone,
		DefaultSurface: speechkit.AssistSurfacePanel,
		DefaultKind:    assist.KindAnswer,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityDate,
		Intent:         shortcuts.IntentDate,
		Label:          "Tell today's date",
		Input:          UtilityInputNone,
		DefaultSurface: speechkit.AssistSurfacePanel,
		DefaultKind:    assist.KindAnswer,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityWeather,
		Intent:         shortcuts.IntentWeather,
		Label:          "Look up the weather forecast",
		Input:          UtilityInputUtterance,
		DefaultSurface: speechkit.AssistSurfacePanel,
		DefaultKind:    assist.KindAnswer,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityTimer,
		Intent:         shortcuts.IntentTimer,
		Label:          "Set a countdown timer",
		Input:          UtilityInputUtterance,
		DefaultSurface: speechkit.AssistSurfaceActionAck,
		DefaultKind:    assist.KindUtilityAction,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityReminder,
		Intent:         shortcuts.IntentReminder,
		Label:          "Set a scheduled reminder",
		Input:          UtilityInputUtterance,
		DefaultSurface: speechkit.AssistSurfaceActionAck,
		DefaultKind:    assist.KindUtilityAction,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityMath,
		Intent:         shortcuts.IntentMath,
		Label:          "Evaluate a math expression",
		Input:          UtilityInputUtterance,
		DefaultSurface: speechkit.AssistSurfacePanel,
		DefaultKind:    assist.KindAnswer,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityWikipedia,
		Intent:         shortcuts.IntentWikipedia,
		Label:          "Summarize a topic from Wikipedia",
		Input:          UtilityInputUtterance,
		DefaultSurface: speechkit.AssistSurfacePanel,
		DefaultKind:    assist.KindAnswer,
		Enabled:        true,
	})
	registry.Register(UtilityDefinition{
		ID:             UtilityTemperature,
		Intent:         shortcuts.IntentTemperature,
		Label:          "Convert a temperature between Celsius, Fahrenheit, and Kelvin",
		Input:          UtilityInputUtterance,
		DefaultSurface: speechkit.AssistSurfacePanel,
		DefaultKind:    assist.KindAnswer,
		Enabled:        true,
	})
	// Home Assistant is listed but disabled by default so settings catalogs
	// report it as opt-in. The Catalog enables it unconditionally: the skill
	// itself fails closed when no bridge is configured, so a recognised
	// smart-home command never becomes a general model prompt.
	registry.Register(UtilityDefinition{
		ID:             UtilityHomeAssistant,
		Intent:         shortcuts.IntentHomeAssistant,
		Label:          "Send command to Home Assistant",
		Input:          UtilityInputUtterance,
		DefaultSurface: speechkit.AssistSurfaceActionAck,
		DefaultKind:    assist.KindUtilityAction,
		Enabled:        false,
	})
	return registry
}

// Register adds or replaces the definition for def.Intent. A definition
// without an intent is ignored; an empty ID defaults to the intent, and empty
// presentation defaults fall back to action_ack / utility_action.
func (r *UtilityRegistry) Register(def UtilityDefinition) {
	if r == nil || def.Intent == shortcuts.IntentNone {
		return
	}
	if r.utilities == nil {
		r.utilities = map[shortcuts.Intent]UtilityDefinition{}
	}
	if def.ID == "" {
		def.ID = UtilityID(def.Intent)
	}
	if def.DefaultSurface == "" {
		def.DefaultSurface = speechkit.AssistSurfaceActionAck
	}
	if def.DefaultKind == "" {
		def.DefaultKind = assist.KindUtilityAction
	}
	r.utilities[def.Intent] = def
}

// Definition returns the enabled definition for intent. The ok return is
// false when the intent is unknown or disabled.
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

// Supports reports whether intent is registered and enabled.
func (r *UtilityRegistry) Supports(intent shortcuts.Intent) bool {
	_, ok := r.Definition(intent)
	return ok
}

// List returns every registered definition, enabled or not, ordered by ID.
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

// clone returns an independent copy so a Catalog can enable entries without
// mutating the registry the host handed in.
func (r *UtilityRegistry) clone() *UtilityRegistry {
	out := NewUtilityRegistry()
	if r == nil {
		return out
	}
	for intent, def := range r.utilities {
		out.utilities[intent] = def
	}
	return out
}
