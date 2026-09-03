//go:build linux

package voiceagent

import "github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"

// Wire protocol constants and shapes for the Voice Agent WebSocket.
//
// These structs are the producer of truth for the Voice Agent wire contract.
// docs/server/fixtures/voiceagent.v1.json is the interchange artifact all
// consumers verify against (pinned here by protocol_fixture_test.go, replayed
// by the TypeScript voiceagent-client and the Android net module);
// docs/server/asyncapi.v1.yaml documents the channel. Adding a message type
// without extending the fixture fails the fixture completeness test.
//
// Control frames are JSON text messages with a required "type" field.
// Audio frames are binary messages containing raw PCM 16kHz S16 mono
// (client → server) or 24kHz S16 mono (server → client, Gemini Live native
// output rate). The first frame a client sends must be a "start" message.

// Client-to-server message types.
const (
	MsgStart        = "start"
	MsgAudioEnd     = "audio_end"
	MsgText         = "text"
	MsgToolResponse = "tool_response"
	MsgPing         = "ping"
	MsgStop         = "stop"
	MsgAdvanceStep  = "advance_step"
	// MsgCancel is the client's tap-to-interrupt: stop relaying the CURRENT
	// agent reply's downlink audio. Idempotent, no payload. The server also
	// invokes the provider-native response cancel where the provider protocol
	// has one (OpenAI Realtime `response.cancel`); Gemini Live, Deepgram
	// Voice Agent, AssemblyAI Voice Agent, and the cascaded pipeline expose
	// no client-side cancel message, so for them the reply keeps generating
	// upstream and the server suppresses its downlink audio until the turn
	// ends. Every cancel — including one that arrives while nothing is
	// playing — is acknowledged with an `interrupted` event frame so client
	// playback state converges.
	MsgCancel = "cancel"
)

const (
	MediaTransportWebSocket = "websocket"
	MediaTransportLiveKit   = "livekit"
)

// Server-to-client message types.
const (
	MsgState            = "state"
	MsgInputTranscript  = "input_transcript"
	MsgOutputTranscript = "output_transcript"
	MsgToolCall         = "tool_call"
	MsgSequenceStep     = "sequence_step"
	MsgEvent            = "event"
	MsgInterrupted      = "interrupted"
	MsgError            = "error"
	MsgSessionEnd       = "session_end"
	MsgPong             = "pong"
)

// Provider-neutral event types surfaced on JSON frames as event_type and
// event_types. These mirror pkg/speechkit/voiceagent/live without forcing
// WebSocket clients to understand Go package names.
const (
	EventSessionReady     = "session_ready"
	EventInputPartial     = "input_partial"
	EventInputFinal       = "input_final"
	EventOutputAudio      = "output_audio"
	EventOutputText       = "output_text"
	EventToolCall         = "tool_call"
	EventToolResultAck    = "tool_result_ack"
	EventInterrupted      = "interrupted"
	EventTurnEnd          = "turn_end"
	EventSessionResumable = "session_resumable"
	EventSessionEnd       = "session_end"
)

// StartFrame is the mandatory first client frame. Fields marked optional
// fall back to persona/role defaults when omitted.
type StartFrame struct {
	Type       string `json:"type"` // must be "start"
	PersonaID  string `json:"persona_id,omitempty"`
	RoleID     string `json:"role_id,omitempty"`
	SequenceID string `json:"sequence_id,omitempty"`
	// Provider selects the realtime backend for THIS session, e.g. "gemini",
	// "deepgram", "assemblyai", "openai", or "cascaded". Empty uses the
	// server's configured default. An unknown or unconfigured provider is rejected at start with a
	// provider_unavailable error. This is what lets a tester switch backends
	// per session ("laufend wechseln") without a server redeploy.
	Provider string `json:"provider,omitempty"`
	// MediaTransport selects where microphone and model audio move. Empty
	// defaults to "websocket" for existing clients. "livekit" keeps this
	// WebSocket as the control channel and moves audio through LiveKit tracks.
	MediaTransport string `json:"media_transport,omitempty"`
	Voice          string `json:"voice,omitempty"`
	Locale         string `json:"locale,omitempty"`
	Model          string `json:"model,omitempty"`
	Thinking       string `json:"thinking,omitempty"` // "off" | "low" | "medium" | "high"
	// Raw activity-detection policy override. Pipeline translates these to
	// the kernel's internal enums.
	ActivityDetection *ActivityDetectionFrame `json:"activity_detection,omitempty"`
	Speaker           *speaker.Options        `json:"speaker,omitempty"`
	// Optional durable instruction layered on top of the role's prompt.
	SystemPromptOverride string `json:"system_prompt_override,omitempty"`
}

// ActivityDetectionFrame maps to voiceagent.ActivityDetectionPolicy.
type ActivityDetectionFrame struct {
	Automatic         bool   `json:"automatic"`
	StartSensitivity  string `json:"start_sensitivity,omitempty"`
	EndSensitivity    string `json:"end_sensitivity,omitempty"`
	PrefixPaddingMs   int32  `json:"prefix_padding_ms,omitempty"`
	SilenceDurationMs int32  `json:"silence_duration_ms,omitempty"`
	ActivityHandling  string `json:"activity_handling,omitempty"`
	TurnCoverage      string `json:"turn_coverage,omitempty"`
}

// TextFrame carries an injected text turn from the client.
type TextFrame struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

// ToolResponseFrame resolves a tool call issued by the server.
type ToolResponseFrame struct {
	Type     string         `json:"type"` // "tool_response"
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

// AdvanceStepFrame asks the server-side workflow runner to move from the
// active sequence step to the next step. StepID is reserved for future direct
// jumps; v1 advances linearly through the authored sequence.
type AdvanceStepFrame struct {
	Type   string `json:"type"` // "advance_step"
	StepID string `json:"step_id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ── server → client ─────────────────────────────────────────────────────────

type StateFrame struct {
	Type string `json:"type"` // "state"
	EventFrameFields
	State string `json:"state"`
	// Provider and MediaTransport are populated on the session_ready frame:
	// the resolved backend that actually serves THIS session (after the
	// start.provider → voice-preference → server-default precedence ran) and
	// the media transport actually applied ("websocket" | "livekit").
	// Clients gate provider-dependent behavior — e.g. sending `cancel` — on
	// these fields being present (kombify-SpeechKit-aajy).
	Provider       string `json:"provider,omitempty"`
	MediaTransport string `json:"media_transport,omitempty"`
}

type TranscriptFrame struct {
	Type string `json:"type"` // "input_transcript" | "output_transcript"
	EventFrameFields
	Text              string  `json:"text"`
	Done              bool    `json:"done"`
	SpeakerLabel      string  `json:"speaker_label,omitempty"`
	PersonID          string  `json:"person_id,omitempty"`
	DisplayName       string  `json:"display_name,omitempty"`
	SpeakerConfidence float64 `json:"speaker_confidence,omitempty"`
}

type ToolCallFrame struct {
	Type string `json:"type"` // "tool_call"
	EventFrameFields
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type SequenceStepFrame struct {
	Type       string `json:"type"` // "sequence_step"
	SequenceID string `json:"sequence_id,omitempty"`
	StepID     string `json:"step_id"`
	StepIndex  int    `json:"step_index,omitempty"`
	Status     string `json:"status"` // "entered" | "completed" | "sequence_completed"
	Reason     string `json:"reason,omitempty"`
}

type EventFrame struct {
	Type string `json:"type"` // "event"
	EventFrameFields
}

type InterruptedFrame struct {
	Type string `json:"type"` // "interrupted"
	EventFrameFields
}

type ErrorFrame struct {
	Type    string `json:"type"` // "error"
	Code    string `json:"code"`
	Message string `json:"message"`
	// Remediation is a short, machine-friendly hint on how to unblock the
	// refused capability. It is emitted for the codes ErrorRemediation
	// covers; codes whose message already is the whole story omit it.
	Remediation string `json:"remediation,omitempty"`
	// RequestID correlates this frame with the server's request log. It is
	// the id the RequestID middleware attached to the WebSocket upgrade, so
	// every error frame of one session carries the same value.
	RequestID string `json:"request_id,omitempty"`
}

// errorRemediation maps error codes to the one action that unblocks them.
// Codes absent from the map carry no remediation: their message already
// names the whole fix, or no client-side action exists.
var errorRemediation = map[string]string{
	"start_required":              "send a start frame as the first message on this socket",
	"provider_unavailable":        "start with a provider this server configured, or omit provider to use its default",
	"invalid_media_transport":     "use media_transport \"websocket\" or \"livekit\"",
	"media_transport_unsupported": "start with a native realtime provider, or use media_transport \"websocket\"",
	"media_transport_unavailable": "configure the LiveKit media bridge, or use media_transport \"websocket\"",
	"audio_transport_mismatch":    "send audio through the LiveKit room, not as binary WebSocket frames",
	"tool_response_unsupported":   "drop tool_response frames for this provider",
}

// ErrorRemediation returns the remediation hint for a wire error code, or ""
// when the code has none. Exported so the handler layer stays the only place
// that decides which codes carry guidance.
func ErrorRemediation(code string) string { return errorRemediation[code] }

type SessionEndFrame struct {
	Type string `json:"type"` // "session_end"
	EventFrameFields
	Reason string `json:"reason"` // "idle" | "go_away" | "client" | "error" | "shutdown" | "max_duration"
}

type PongFrame struct {
	Type string `json:"type"` // "pong"
}

type EventFrameFields struct {
	AISessionID      string         `json:"ai_session_id,omitempty"`
	EventType        string         `json:"event_type,omitempty"`
	EventTypes       []string       `json:"event_types,omitempty"`
	ProviderMetadata map[string]any `json:"provider_metadata,omitempty"`
}

// envelope is a lightweight struct for peeking at incoming frames before
// fully decoding them.
type envelope struct {
	Type string `json:"type"`
}
