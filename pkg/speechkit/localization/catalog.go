// Package localization resolves stable SpeechKit message IDs against the
// repository-owned locale catalogs.
package localization

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/language"
)

// MessageID is a stable semantic identifier for customer-facing prose.
type MessageID string

const (
	CompanionHomeAssistantNotConfigured MessageID = "companion.home_assistant.not_configured"
	CompanionHomeAssistantCommandEmpty  MessageID = "companion.home_assistant.command.empty"
	CompanionHomeAssistantNotMatched    MessageID = "companion.home_assistant.command.not_matched"
	CompanionHomeAssistantRejected      MessageID = "companion.home_assistant.command.rejected"
	CompanionHomeAssistantUnavailable   MessageID = "companion.home_assistant.unavailable"
)

var messageIDs = []MessageID{
	CompanionHomeAssistantNotConfigured,
	CompanionHomeAssistantCommandEmpty,
	CompanionHomeAssistantNotMatched,
	CompanionHomeAssistantRejected,
	CompanionHomeAssistantUnavailable,
}

var supportedLocales = []string{"en", "de", "es", "zh-Hans", "hi", "ar"}

// The non-English catalogs are translation proposals. Their presence does not
// constitute the human-review or signed Wave 2 localization evidence required
// by the workspace localization standard.
//
//go:embed catalogs/*.json
var catalogFiles embed.FS

var catalogs = mustLoadCatalogs()

// SupportedLocales returns the initial SpeechKit locale set. English is the
// source locale and final fallback.
func SupportedLocales() []string {
	return append([]string(nil), supportedLocales...)
}

// ResolveLocale performs deterministic BCP-47 negotiation for the registered
// catalogs. Supported regional variants fall back to their registered base
// language. Simplified Chinese variants resolve to zh-Hans; traditional
// Chinese and unsupported languages fall back to English.
func ResolveLocale(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return "en"
	}
	tag, err := language.Parse(locale)
	if err != nil {
		return "en"
	}
	base, _ := tag.Base()
	switch base.String() {
	case "en", "de", "es", "hi", "ar":
		return base.String()
	case "zh":
		_, rawScript, _ := tag.Raw()
		if rawScript.String() != "Zzzz" {
			if rawScript.String() == "Hans" {
				return "zh-Hans"
			}
			return "en"
		}
		script, confidence := tag.Script()
		if confidence != language.No && script.String() == "Hans" {
			return "zh-Hans"
		}
		return "en"
	default:
		return "en"
	}
}

// Message is one catalog result with the negotiated locale that must also be
// used for result metadata and TTS.
type Message struct {
	ID     MessageID
	Locale string
	Text   string
}

// Resolve negotiates the locale once and resolves id from that exact catalog.
func Resolve(locale string, id MessageID) Message {
	resolvedLocale := ResolveLocale(locale)
	text := catalogs[resolvedLocale][id]
	if text == "" {
		resolvedLocale = "en"
		text = catalogs[resolvedLocale][id]
	}
	return Message{ID: id, Locale: resolvedLocale, Text: text}
}

// Text returns the localized text for id. Missing locale entries fall back to
// English. An unknown message ID returns an empty string so callers cannot
// accidentally surface the identifier itself as customer prose.
func Text(locale string, id MessageID) string {
	return Resolve(locale, id).Text
}

func mustLoadCatalogs() map[string]map[MessageID]string {
	loaded := make(map[string]map[MessageID]string, len(supportedLocales))
	for _, locale := range supportedLocales {
		path := fmt.Sprintf("catalogs/%s.json", locale)
		raw, err := catalogFiles.ReadFile(path)
		if err != nil {
			panic(fmt.Sprintf("speechkit localization: read %s: %v", path, err))
		}
		catalog, err := decodeCatalog(raw)
		if err != nil {
			panic(fmt.Sprintf("speechkit localization: decode %s: %v", path, err))
		}
		if len(catalog) != len(messageIDs) {
			panic(fmt.Sprintf("speechkit localization: %s contains %d messages, want %d", path, len(catalog), len(messageIDs)))
		}
		for _, id := range messageIDs {
			if strings.TrimSpace(catalog[id]) == "" {
				panic(fmt.Sprintf("speechkit localization: %s is missing %q", path, id))
			}
		}
		loaded[locale] = catalog
	}
	return loaded
}

func decodeCatalog(raw []byte) (map[MessageID]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := opening.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("catalog root must be an object")
	}

	catalog := make(map[MessageID]string, len(messageIDs))
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		keyText, ok := key.(string)
		if !ok {
			return nil, fmt.Errorf("catalog key must be a string")
		}
		id := MessageID(keyText)
		if !knownMessageID(id) {
			return nil, fmt.Errorf("unknown message ID %q", id)
		}
		if _, duplicate := catalog[id]; duplicate {
			return nil, fmt.Errorf("duplicate message ID %q", id)
		}
		var text string
		if err := decoder.Decode(&text); err != nil {
			return nil, fmt.Errorf("message %q: %w", id, err)
		}
		catalog[id] = text
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := closing.(json.Delim); !ok || delim != '}' {
		return nil, fmt.Errorf("catalog object is not closed")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("catalog contains a trailing JSON value")
		}
		return nil, fmt.Errorf("catalog trailing data: %w", err)
	}
	return catalog, nil
}

func knownMessageID(candidate MessageID) bool {
	for _, id := range messageIDs {
		if candidate == id {
			return true
		}
	}
	return false
}
