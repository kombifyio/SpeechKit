package catalog

// ThinkModel is one managed think (LLM) leg of a native voice agent: the
// provider type and model id the voice-agent API itself runs.
type ThinkModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// deepgramThinkChain is the catalog default think chain of the Deepgram Voice
// Agent profile (realtime.deepgram.voice-agent): the first entry is the
// default, each later entry the next fallback a failing turn moves to. Both
// are Deepgram-managed models, so the deployment needs no think credential.
var deepgramThinkChain = []ThinkModel{
	{Provider: "open_ai", Model: "gpt-4o-mini"},
	{Provider: "open_ai", Model: "gpt-4.1-mini"},
}

// VoiceAgentThinkChain returns the catalog default think chain of a voice
// agent profile, default first, or nil when the profile's think leg is not
// catalog-managed. Callers receive a copy.
func VoiceAgentThinkChain(profileID string) []ThinkModel {
	switch profileID {
	case "realtime.deepgram.voice-agent":
		return append([]ThinkModel(nil), deepgramThinkChain...)
	default:
		return nil
	}
}
