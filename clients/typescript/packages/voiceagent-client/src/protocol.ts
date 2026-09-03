// Frame types for the SpeechKit Voice Agent WebSocket.
//
// Source of truth: the Go structs in internal/server/voiceagent/protocol.go
// (the producer). docs/server/fixtures/voiceagent.v1.json is the interchange
// artifact all consumers verify against (here: protocol.fixture.test.ts);
// docs/server/asyncapi.v1.yaml documents the channel.
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

/**
 * Speaker diarization / identification options for the session. Mirrors
 * `speaker.Options` on the server; field names are camelCase there, unlike
 * the snake_case frame fields around them.
 */
export interface SpeakerOptions {
  enabled?: boolean;
  diarization?: boolean;
  identification?: boolean;
  attribution?: boolean;
  providerProfileId?: string;
  model?: string;
  diarizationModel?: string;
  language?: string;
  speakersExpected?: number;
  minSpeakersExpected?: number;
  maxSpeakersExpected?: number;
  speakerType?: string;
  knownValues?: string[];
  knownSpeakers?: Array<{ id?: string; displayName?: string; [key: string]: unknown }>;
  preferStreaming?: boolean;
  allowProviderMapping?: boolean;
}

export interface StartFrame {
  type: "start";
  persona_id?: string;
  role_id?: string;
  sequence_id?: string;
  /**
   * Selects the realtime backend for THIS session, e.g. "gemini",
   * "openai", "deepgram", "assemblyai", "cascaded". Empty uses the
   * server's configured default. An unknown or unconfigured provider is
   * rejected at start with a provider_unavailable error.
   */
  provider?: string;
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
  /** Speaker diarization / identification for this session. */
  speaker?: SpeakerOptions;
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

/**
 * Body-less client control frames.
 *
 * `cancel` is tap-to-interrupt: it stops the CURRENT agent reply's downlink
 * audio. It is idempotent and always acknowledged with an `interrupted`
 * frame, even when nothing was playing, so client playback state converges.
 * Whether the reply also stops generating upstream is provider-dependent;
 * the server suppresses its downlink either way.
 */
export interface SimpleClientFrame {
  type: "audio_end" | "ping" | "stop" | "cancel";
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
  ai_session_id?: string;
  event_type?: LiveEventType;
  event_types?: LiveEventType[];
  provider_metadata?: Record<string, unknown>;
}

export interface StateFrame extends ServerFrameMeta {
  type: "state";
  state: AgentState;
  /**
   * Realtime backend actually serving this session. Present on the
   * `session_ready` frame only, after the server resolved
   * `start.provider` → voice preference → server default.
   */
  provider?: string;
  /** Media transport actually applied. Present on `session_ready` only. */
  media_transport?: "websocket" | "livekit";
}

export interface TranscriptFrame extends ServerFrameMeta {
  type: "input_transcript" | "output_transcript";
  /**
   * Streaming granularity is provider-dependent within v1: some backends
   * send the CUMULATIVE turn text on every frame, others send deltas.
   * Hosts that need cumulative text (e.g. teleprompter turn views) should
   * normalize with {@link accumulateTranscript} instead of assuming one
   * form.
   */
  text: string;
  done: boolean;
  /**
   * Diarization label for the voice this text came from, e.g. `S1`. Present
   * only when the session started with speaker options and the provider
   * returned an attribution.
   */
  speaker_label?: string;
  /** Identified person behind `speaker_label`, when identification ran. */
  person_id?: string;
  /** Human-readable name for `person_id`. */
  display_name?: string;
  /** Confidence in the attribution, 0..1. */
  speaker_confidence?: number;
}

/**
 * Normalizes streaming transcript text to the cumulative form regardless
 * of whether the provider streams cumulative snapshots or deltas: feed
 * every frame's `text` through it with the previous accumulated value.
 * Reset the accumulator to "" when a frame arrives with `done: true`.
 */
export function accumulateTranscript(previous: string, next: string): string {
  if (!previous) return next;
  if (!next) return previous;
  if (next.startsWith(previous)) return next; // cumulative stream
  if (previous.endsWith(next)) return previous; // duplicate tail
  return previous + next; // delta stream
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
  ai_session_id?: string;
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
