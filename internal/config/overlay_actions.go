package config

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

const (
	OverlayActionCopy     = "copy"
	OverlayActionNote     = "note"
	OverlayActionLanguage = "language"
	OverlayActionMeeting  = "meeting"
)

// DefaultOverlayActions is the overlay strip a fresh install shows.
func DefaultOverlayActions() []string {
	return []string{
		OverlayActionCopy,
		OverlayActionNote,
		OverlayActionLanguage,
		OverlayActionMeeting,
	}
}

// NormalizeOverlayActions keeps known action IDs in display order.
// An empty list means "use the default strip", not "hide everything".
func NormalizeOverlayActions(actions []string) []string {
	allowed := map[string]struct{}{
		OverlayActionCopy:     {},
		OverlayActionNote:     {},
		OverlayActionLanguage: {},
		OverlayActionMeeting:  {},
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(DefaultOverlayActions()))
	for _, raw := range actions {
		id := strings.ToLower(strings.TrimSpace(raw))
		if _, ok := allowed[id]; !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return DefaultOverlayActions()
	}
	return out
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
