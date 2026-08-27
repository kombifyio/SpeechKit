package config

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

const (
	OverlayActionMic      = "mic"
	OverlayActionCopy     = "copy"
	OverlayActionNote     = "note"
	OverlayActionLanguage = "language"
	OverlayActionLive     = "live"
	OverlayActionModel    = "model"
	OverlayActionMeeting  = "meeting"
	// OverlayActionNone persists an empty shortcut strip. A missing
	// overlay_actions key still means "use the shipped default".
	OverlayActionNone = "none"
)

var overlayActionOrder = []string{
	OverlayActionMic,
	OverlayActionCopy,
	OverlayActionNote,
	OverlayActionLanguage,
	OverlayActionLive,
	OverlayActionModel,
	OverlayActionMeeting,
}

// DefaultOverlayActions is the overlay strip a fresh install shows.
func DefaultOverlayActions() []string {
	return append([]string(nil), overlayActionOrder...)
}

func knownOverlayAction(id string) bool {
	switch id {
	case OverlayActionMic, OverlayActionCopy, OverlayActionNote, OverlayActionLanguage, OverlayActionLive, OverlayActionModel, OverlayActionMeeting:
		return true
	default:
		return false
	}
}

func isLegacyDefaultStrip(enabled map[string]struct{}) bool {
	if len(enabled) != 4 {
		return false
	}
	for _, id := range []string{OverlayActionCopy, OverlayActionNote, OverlayActionLanguage, OverlayActionMeeting} {
		if _, ok := enabled[id]; !ok {
			return false
		}
	}
	return true
}

// NormalizeOverlayActions keeps known action IDs in display order.
// A nil list means "use the shipped default". The none sentinel or an
// explicit empty list means "show no shortcut buttons".
func NormalizeOverlayActions(actions []string) []string {
	if actions == nil {
		return DefaultOverlayActions()
	}
	enabled := make(map[string]struct{}, len(overlayActionOrder))
	sawNone := false
	for _, raw := range actions {
		id := strings.ToLower(strings.TrimSpace(raw))
		if id == "" {
			continue
		}
		if id == OverlayActionNone {
			sawNone = true
			continue
		}
		if knownOverlayAction(id) {
			enabled[id] = struct{}{}
		}
	}
	if len(enabled) == 0 {
		if sawNone || len(actions) == 0 {
			return []string{}
		}
		return DefaultOverlayActions()
	}
	if _, hasMic := enabled[OverlayActionMic]; !hasMic && isLegacyDefaultStrip(enabled) {
		enabled[OverlayActionMic] = struct{}{}
	}
	out := make([]string, 0, len(overlayActionOrder))
	for _, id := range overlayActionOrder {
		if _, ok := enabled[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

// PersistOverlayActions is the list written to config.toml. An empty
// runtime strip is stored as the none sentinel so a reload does not
// fall back to the shipped default.
func PersistOverlayActions(actions []string) []string {
	normalized := NormalizeOverlayActions(actions)
	if len(normalized) == 0 {
		return []string{OverlayActionNone}
	}
	return normalized
}

// OverlayActionEnabled reports whether id is in the configured strip.
func OverlayActionEnabled(actions []string, id string) bool {
	want := strings.ToLower(strings.TrimSpace(id))
	for _, action := range NormalizeOverlayActions(actions) {
		if action == want {
			return true
		}
	}
	return false
}

// NormalizeMainLanguage is the locale the overlay language switch rotates
// back to after English. Multilanguage and empty fall through to German,
// which is the product's default spoken locale.
func NormalizeMainLanguage(language string) string {
	trimmed := strings.ToLower(strings.TrimSpace(language))
	switch trimmed {
	case "", "auto", stt.LanguageMulti:
		return "de"
	}
	if i := strings.IndexByte(trimmed, '-'); i > 0 {
		trimmed = trimmed[:i]
	}
	if len(trimmed) != 2 {
		return "de"
	}
	return trimmed
}

// CurrentSpeechLanguage is the value the overlay language switch shows:
// the multilanguage sentinel when nothing is pinned, otherwise the pin.
func CurrentSpeechLanguage(language string, detect bool) string {
	if detect {
		return stt.LanguageMulti
	}
	trimmed := strings.ToLower(strings.TrimSpace(language))
	switch trimmed {
	case "", "auto", stt.LanguageMulti:
		return stt.LanguageMulti
	}
	return trimmed
}

// NextSpeechLanguage rotates multilanguage → English → the user's main
// language, then back. When the main language is English the cycle has two
// stops.
func NextSpeechLanguage(current, main string) string {
	cur := CurrentSpeechLanguage(current, false)
	own := NormalizeMainLanguage(main)
	switch cur {
	case stt.LanguageMulti:
		return "en"
	case "en":
		if own == "en" {
			return stt.LanguageMulti
		}
		return own
	default:
		return stt.LanguageMulti
	}
}

const (
	DictateDeepgramProfileID   = "stt.deepgram.nova-3"
	DictateAssemblyAIProfileID = "stt.assemblyai.universal"
)

// DictationModeIsLive reports whether processing streams while the user speaks.
func DictationModeIsLive(mode string) bool {
	return NormalizeDictationProcessingMode(mode, DictationProcessingModeAuto) != DictationProcessingModeFinalFull
}

// ToggleDictationProcessingMode flips live (auto) and full-capture.
func ToggleDictationProcessingMode(mode string) string {
	if DictationModeIsLive(mode) {
		return DictationProcessingModeFinalFull
	}
	return DictationProcessingModeAuto
}

// NextDictateSTTProfile cycles Deepgram Nova-3 and AssemblyAI Universal 3.5.
func NextDictateSTTProfile(current string) (primary, fallback string) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(current)), "stt.assemblyai.") {
		return DictateDeepgramProfileID, DictateAssemblyAIProfileID
	}
	return DictateAssemblyAIProfileID, DictateDeepgramProfileID
}
