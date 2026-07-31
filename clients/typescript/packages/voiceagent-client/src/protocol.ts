// Frame types for the SpeechKit Voice Agent WebSocket. Mirrors
// docs/server/asyncapi.v1.yaml and internal/server/voiceagent/protocol.go.
// Field guarantees: additive within v1; no rename/removal without v2.

// ── Client → server frames ───────────────────────────────────────────────

export interface ActivityDetectionFrame {
  automatic: boolean;
  start_sensitivity?: "" | "low" | "medium" | "high";
  end_sensitivity?: "" | "low" | "medium" | "high";
  prefix_padding_ms?: number;
  silence_duration_ms?: number;
  activity_handling?: "" | "no_interrupt" | "start_of_activity_interrupts";
  turn_coverage?:
    | ""
    | "turn_includes_only_activity"
    | "turn_includes_all_input"
    | "turn_includes_audio_activity";
}

export interface StartFrame {
  type: "start";
  persona_id?: string;
  role_id?: string;
  sequence_id?: string;
  /**
   * Where microphone and model audio move. `websocket` (default) keeps the
   * v1 binary-frame audio path; `livekit` keeps this WebSocket as the
   * control channel and moves audio through the LiveKit room minted by
   * `POST /v1/voiceagent/sessions`.
   */
  media_transport?: "websocket" | "livekit";
  voice?: string;
  locale?: string;
  model?: string;
  thinking?: "off" | "low" | "medium" | "high";
  activity_detection?: ActivityDetectionFrame;
  system_prompt_override?: string;
}

export interface TextFrame {
  type: "text";
  text: string;
}

export interface ToolResponseFrame {
  type: "tool_response";
  id: string;
  name: string;
  response: Record<string, unknown>;
}

export interface AdvanceStepFrame {
  type: "advance_step";
  step_id?: string;
  reason?: string;
}

export interface SimpleClientFrame {
  type: "audio_end" | "ping" | "stop";
}

export type ClientFrame = StartFrame | TextFrame | ToolResponseFrame | AdvanceStepFrame | SimpleClientFrame;

// ── Server → client frames ───────────────────────────────────────────────

export type AgentState =
  | "connecting"
  | "listening"
  | "processing"
  | "speaking"
  | "recovering"
  | "deactivating"
  | "inactive";

/** SpeechKit-normalized live event meaning attached to server frames. */
export type LiveEventType =
  | "session_ready"
  | "input_partial"
  | "input_final"
  | "output_audio"
  | "output_text"
  | "tool_call"
  | "tool_result_ack"
  | "interrupted"
  | "turn_end"
  | "session_resumable"
  | "session_end";

/**
 * Optional event metadata carried by server frames (additive within v1).
 * `provider_metadata` exposes provider-native event details for
 * diagnostics and advanced hosts.
 */
export interface ServerFrameMeta {
  event_type?: LiveEventType;
  event_types?: LiveEventType[];
  provider_metadata?: Record<string, unknown>;
}

export interface StateFrame extends ServerFrameMeta {
  type: "state";
  state: AgentState;
}

export interface TranscriptFrame extends ServerFrameMeta {
  type: "input_transcript" | "output_transcript";
  text: string;
  done: boolean;
}

export interface ToolCallFrame extends ServerFrameMeta {
  type: "tool_call";
  id: string;
  name: string;
  args?: Record<string, unknown>;
}

export interface SequenceStepFrame {
  type: "sequence_step";
  sequence_id?: string;
  step_id: string;
  step_index?: number;
  status: "entered" | "completed" | "sequence_completed";
  reason?: string;
}

/** Provider event frame without a more specific v1 mapping. */
export interface EventFrame extends ServerFrameMeta {
  type: "event";
  event_type: LiveEventType;
}

export interface InterruptedFrame extends ServerFrameMeta {
  type: "interrupted";
}

export interface ErrorFrame {
  type: "error";
  code: string;
  message: string;
  /**
   * Short machine-friendly hint on how to unblock the denied capability
   * (additive, optional).
   */
  remediation?: string;
  /**
   * Gateway/server correlation id for support and tracing (additive,
   * optional).
   */
  request_id?: string;
}

export interface SessionEndFrame extends ServerFrameMeta {
  type: "session_end";
  reason: "idle" | "go_away" | "client" | "error" | "shutdown" | "max_duration";
}

export interface PongFrame {
  type: "pong";
}

export type ServerFrame =
  | StateFrame
  | TranscriptFrame
  | ToolCallFrame
  | SequenceStepFrame
  | EventFrame
  | InterruptedFrame
  | ErrorFrame
  | SessionEndFrame
  | PongFrame;

// ── Audio constants ──────────────────────────────────────────────────────

/** Sample rate clients must send at: 16 kHz signed-int16 mono PCM. */
export const CLIENT_SAMPLE_RATE = 16_000;

/** Sample rate the server emits at: 24 kHz signed-int16 mono PCM. */
export const SERVER_SAMPLE_RATE = 24_000;

// ── Session ticket ───────────────────────────────────────────────────────

/**
 * Prefix of the WebSocket subprotocol carrying the upgrade ticket:
 * `Sec-WebSocket-Protocol: ticket.<ticket>`. The server authenticates the
 * upgrade exclusively through this subprotocol; tickets never belong in
 * URLs.
 */
export const TICKET_SUBPROTOCOL_PREFIX = "ticket.";

export interface SessionTicket {
  session_id: string;
  ticket: string;
  ws_url?: string;
  /**
   * WebSocket subprotocol value to pass during upgrade, e.g.
   * `ticket.<ticket>`. When absent, derive it with
   * `TICKET_SUBPROTOCOL_PREFIX + ticket`.
   */
  ws_subprotocol?: string;
  expires_at?: string;
}
