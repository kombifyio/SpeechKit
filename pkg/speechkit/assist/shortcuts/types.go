package shortcuts

// Intent is the stable identifier of a recognised codeword utility. The
// value doubles as the wire id hosts see in AssistResult.ShortcutID and in
// the utility registry of pkg/speechkit/assist/skills.
type Intent string

// Built-in intents. Text utilities (copy, insert, summarize, quick note) are
// executed by the host; the Voice-Companion intents are answered by the
// skill catalog in pkg/speechkit/assist/skills/companion.
const (
	IntentNone       Intent = ""
	IntentQuickNote  Intent = "quick_note"
	IntentCopyLast   Intent = "copy_last"
	IntentInsertLast Intent = "insert_last"
	IntentSummarize  Intent = "summarize"

	// Voice-Companion intents (used by Assist-Mode when wakeword.default_mode
	// = "assist"). See docs/voice-companion.md for the full surface and
	// pkg/speechkit/assist/skills/companion for the per-intent executors.
	IntentTime          Intent = "time"
	IntentDate          Intent = "date"
	IntentWeather       Intent = "weather"
	IntentTimer         Intent = "timer"
	IntentReminder      Intent = "reminder"
	IntentMath          Intent = "math"
	IntentWikipedia     Intent = "wikipedia"
	IntentTemperature   Intent = "temperature"
	IntentHomeAssistant Intent = "home_assistant"
)

// Resolution is the outcome of matching one transcript against the
// registry: the matched Intent (IntentNone when nothing matched), the
// payload that followed the matched phrase, and the normalised alias that
// matched.
type Resolution struct {
	Intent  Intent
	Payload string
	Alias   string
}

// Phrase is one registered utterance for an intent. Prefix phrases also
// match when followed by a payload ("summarize this <payload>");
// NoSpacePrefix allows a payload without a separating space for scripts
// that do not use word spacing. Higher Priority phrases are tried first.
type Phrase struct {
	Value         string `json:"value"`
	Prefix        bool   `json:"prefix"`
	NoSpacePrefix bool   `json:"no_space_prefix,omitempty"`
	Priority      int    `json:"priority,omitempty"`
}

// IntentLexicon groups the phrases of one intent for one BCP-47 locale.
// An empty Locale registers the phrases for every locale.
type IntentLexicon struct {
	Intent  Intent
	Locale  string
	Phrases []Phrase
}
