// Package cascaded implements a turn-based STT -> LLM -> TTS voice agent
// provider. It is the self-hosted-friendly alternative to real-time
// providers like Gemini Live and Moshi.
//
// The package is platform-neutral pure Go. Server-Target (Linux container)
// and Device-Target (Windows Wails reference UI) both wrap it: see
// internal/server/voiceagent.CascadedProvider for the Linux server's
// adapter, and internal/voiceagent.LocalVoiceAgentProvider for the
// Device-Target's adapter.
//
// Turn detection uses energy-based silence detection (RMS of the 16 kHz S16
// PCM stream). Callers that want tighter control can send an audio_end
// frame which triggers immediate processing regardless of the silence
// heuristic.
package cascaded

// SessionConfig is the minimal per-session configuration the cascaded
// provider needs. Adapters in internal/server/voiceagent and
// internal/voiceagent translate their richer config types into this
// when calling Connect / UpdateInstructions.
type SessionConfig struct {
	Locale           string
	Voice            string
	SystemPrompt     string
	RefinementPrompt string
}

// Message is the subset of voice-agent message fields the cascaded path
// emits. Adapters wrap this into their richer LiveMessage type when
// forwarding to clients.
type Message struct {
	Audio                []byte
	InputTranscript      string
	InputTranscriptDone  bool
	OutputTranscript     string
	OutputTranscriptDone bool
}
