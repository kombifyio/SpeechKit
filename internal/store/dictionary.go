package store

import "strings"

const userDictionarySettingsSource = "settings"

func normalizeDictionaryLanguage(language string) string {
	language = strings.TrimSpace(strings.ToLower(language))
	if language == "" {
		return ""
	}
	if idx := strings.IndexAny(language, "-_"); idx > 0 {
		return language[:idx]
	}
	return language
}

func normalizeUserDictionaryEntry(entry UserDictionaryEntry, language string) (UserDictionaryEntry, bool) {
	entry.Spoken = strings.TrimSpace(entry.Spoken)
	entry.Canonical = strings.TrimSpace(entry.Canonical)
	entry.Language = normalizeDictionaryLanguage(entry.Language)
	if entry.Language == "" {
		entry.Language = normalizeDictionaryLanguage(language)
	}
	entry.Source = strings.TrimSpace(strings.ToLower(entry.Source))
	if entry.Source == "" {
		entry.Source = userDictionarySettingsSource
	}
	entry.Enabled = true
	return entry, entry.Spoken != "" && entry.Canonical != ""
}
