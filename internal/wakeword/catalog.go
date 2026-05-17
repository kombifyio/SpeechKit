package wakeword

import "strings"

// PhraseCatalogEntry describes a curated wake-phrase shipped with SpeechKit.
// Under the sherpa-onnx KWS backend the actual detection contract is the
// keywords.txt file in the bundled KWS model directory (BPE-tokenised, one
// keyword per line with optional `:boost` and trailing `@display-label`).
// The catalog here exists only to:
//
//   - Give the settings UI a stable ID/label mapping that survives across
//     config saves (the user picks "Hey Quby" from a dropdown, we persist
//     "hey_quby" in the TOML; the adapter then resolves whatever the
//     bundled keywords.txt contains for that phrase).
//   - Surface tradeoff notes the UI can render next to each entry.
//
// Adding a new entry is non-breaking. Removing or renaming requires a
// config migration.
type PhraseCatalogEntry struct {
	// ID is the stable identifier saved in [wakeword] phrase_id. Lowercase
	// snake_case. Never change after publishing — config files in the wild
	// reference it.
	ID string

	// DisplayName is the human-readable label rendered in the tray and
	// settings UI. Free-form; can include parenthetical pronunciation
	// hints (e.g. "Hey Quby (Cubi / Kubi)").
	DisplayName string

	// KeywordLabel is the @-suffix that must appear at the end of the
	// matching line in keywords.txt. The sherpa-onnx KeywordSpotter reports
	// hits via this label, which the pipeline forwards as DetectionEvent.Keyword.
	// When empty, sherpa reports the raw BPE-decoded keyword.
	KeywordLabel string

	// Notes is a one-line tradeoff summary surfaced in the settings UI
	// next to the entry. Plain text, no markup.
	Notes string
}

// DefaultCatalog is SpeechKit's curated wake-phrase list. The corresponding
// BPE-tokenised keyword lines live in
// dist/windows/SpeechKit/wakeword-kws/keywords.txt; adding a new entry here
// requires adding the matching line in that file (and any per-platform
// bundle counterpart).
//
// Selection criteria (consensus across Picovoice / openWakeWord / sherpa
// literature):
//   - 3-4 syllables (disambiguates without feeling sluggish)
//   - distinct consonant onsets (K, T, P, B help the encoder)
//   - vowel diversity
//   - rare in everyday conversation (low false-accept rate)
//   - pronounceable across DE+EN (SpeechKit's primary languages)
func DefaultCatalog() []PhraseCatalogEntry {
	return []PhraseCatalogEntry{
		{
			ID:           "hey_quby",
			DisplayName:  "Hey Quby (Cubi / Kubi)",
			KeywordLabel: "hey_quby",
			Notes:        "SpeechKit brand default. Distinct K/B consonants, varied vowels.",
		},
		{
			ID:           "hey_computer",
			DisplayName:  "Hey Computer",
			KeywordLabel: "hey_computer",
			Notes:        "Star Trek classic. 4 syllables, very distinct phonemes.",
		},
		{
			ID:           "hey_jarvis",
			DisplayName:  "Hey Jarvis",
			KeywordLabel: "hey_jarvis",
			Notes:        "Marvel-popular. Strong J/RV/S consonants.",
		},
		{
			ID:           "hey_mira",
			DisplayName:  "Hey Mira",
			KeywordLabel: "hey_mira",
			Notes:        "Short brand-style alternative; equally natural in DE+EN.",
		},
		{
			ID:           "hey_kombify",
			DisplayName:  "Hey Kombify",
			KeywordLabel: "hey_kombify",
			Notes:        "Organisation brand. 4 syllables, three distinct consonant onsets (K/B/F).",
		},
	}
}

// LookupPhrase returns the catalog entry matching id, or nil if not found.
// The lookup is case-insensitive and trims surrounding whitespace so the
// stored config value tolerates manual edits.
func LookupPhrase(id string) *PhraseCatalogEntry {
	key := strings.ToLower(strings.TrimSpace(id))
	if key == "" {
		return nil
	}
	for _, entry := range DefaultCatalog() {
		if entry.ID == key {
			out := entry
			return &out
		}
	}
	return nil
}
