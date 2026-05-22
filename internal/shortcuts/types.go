package shortcuts

type Intent string

const (
	IntentNone       Intent = ""
	IntentQuickNote  Intent = "quick_note"
	IntentCopyLast   Intent = "copy_last"
	IntentInsertLast Intent = "insert_last"
	IntentSummarize  Intent = "summarize"

	// Voice-Companion intents (used by Assist-Mode when wakeword.default_mode
	// = "assist"). See docs/voice-companion.md for the full surface and
	// internal/assist/skills/voice_companion/ for the per-intent executors.
	// Skills land in Phase 1; the intents are wired here in Phase 0 so the
	// resolver catalog and tests can reference stable values.
	IntentTime          Intent = "time"
	IntentDate          Intent = "date"
	IntentWeather       Intent = "weather"
	IntentTimer         Intent = "timer"
	IntentReminder      Intent = "reminder"
	IntentMath          Intent = "math"
	IntentWikipedia     Intent = "wikipedia"
	IntentHomeAssistant Intent = "home_assistant"
)

type Resolution struct {
	Intent  Intent
	Payload string
	Alias   string
}

type Phrase struct {
	Value    string
	Prefix   bool
	Priority int
}

type IntentLexicon struct {
	Intent  Intent
	Locale  string
	Phrases []Phrase
}
