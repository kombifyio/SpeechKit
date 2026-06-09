//go:build linux

package voiceagent

import "github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"

// Wire protocol constants and shapes for the Voice Agent WebSocket.
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
	MsgInterrupted      = "interrupted"
	MsgError            = "error"
	MsgSessionEnd       = "session_end"
	MsgPong             = "pong"
)

// StartFrame is the mandatory first client frame. Fields marked optional
// fall back to persona/role defaults when omitted.
type StartFrame struct {
	Type       string `json:"type"` // must be "start"
	PersonaID  string `json:"persona_id,omitempty"`
	RoleID     string `json:"role_id,omitempty"`
	SequenceID string `json:"sequence_id,omitempty"`
	// Provider selects the realtime backend for THIS session, e.g. "deepgram",
	// "gemini", or "cascaded". Empty uses the server's configured default. An
	// unknown or unconfigured provider is rejected at start with a
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
	Type  string `json:"type"` // "state"
	State string `json:"state"`
}

type TranscriptFrame struct {
	Type              string  `json:"type"` // "input_transcript" | "output_transcript"
	Text              string  `json:"text"`
	Done              bool    `json:"done"`
	SpeakerLabel      string  `json:"speaker_label,omitempty"`
	PersonID          string  `json:"person_id,omitempty"`
	DisplayName       string  `json:"display_name,omitempty"`
	SpeakerConfidence float64 `json:"speaker_confidence,omitempty"`
}

type ToolCallFrame struct {
	Type string         `json:"type"` // "tool_call"
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

type InterruptedFrame struct {
	Type string `json:"type"` // "interrupted"
}

type ErrorFrame struct {
	Type    string `json:"type"` // "error"
	Code    string `json:"code"`
	Message string `json:"message"`
}

type SessionEndFrame struct {
	Type   string `json:"type"`   // "session_end"
	Reason string `json:"reason"` // "idle" | "go_away" | "client" | "error" | "shutdown"
}

type PongFrame struct {
	Type string `json:"type"` // "pong"
}

// envelope is a lightweight struct for peeking at incoming frames before
// fully decoding them.
type envelope struct {
	Type string `json:"type"`
}
