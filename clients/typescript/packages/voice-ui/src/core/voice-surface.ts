/**
 * Vendored canonical `speechkit.voice_surface.v1` contract types and reducer.
 *
 * The kit is UI-only: it renders this event stream and owns no session FSM.
 * This module must stay byte-consistent in shape with the canonical copies in
 * the SpeechKit desktop frontend (`frontend/app/src/lib/voice-surface-contract.ts`)
 * and `@kombify/ai-sdk` — `scripts/check-contract-drift.mjs` gates drift in the
 * private repo CI.
 */

export const SPEECHKIT_VOICE_SURFACE_VERSION = "speechkit.voice_surface.v1" as const;
export const SPEECHKIT_VOICE_UI_VERSION = "speechkit.voice_ui.v1" as const;

export const SPEECHKIT_VOICE_MODES = ["dictation", "assist", "voice_agent"] as const;
export const SPEECHKIT_CAPTURE_POLICIES = [
  "push_to_talk",
  "explicit_record",
  "hotkey_local",
  "wakeword_local",
  "device_agent"
] as const;
export const SPEECHKIT_TRANSPORTS = [
  "local_http",
  "gateway_http",
  "voiceagent_ws_ticket",
  "livekit_optional"
] as const;
export const SPEECHKIT_VOICE_EVENT_TYPES = [
  "voice.capture_started",
  "voice.capture_stopped",
  "voice.transcript_draft",
  "voice.transcript_final",
  "voice.assist_result",
  "voice.agent_turn",
  "voice.tts_started",
  "voice.tts_finished",
  "voice.cancelled",
  "voice.barge_in",
  "voice.denied"
] as const;

export type SpeechKitVoiceMode = (typeof SPEECHKIT_VOICE_MODES)[number];
export type SpeechKitCapturePolicy = (typeof SPEECHKIT_CAPTURE_POLICIES)[number];
export type SpeechKitTransport = (typeof SPEECHKIT_TRANSPORTS)[number];
export type SpeechKitVoiceEventType = (typeof SPEECHKIT_VOICE_EVENT_TYPES)[number];
export type SpeechKitVoiceSurface =
  | "mobile"
  | "workbench"
  | "floating_panel"
  | "embed"
  | "device_agent";
export type SpeechKitProviderKind = "local" | "byok" | "kombify_managed";
export type SpeechKitMediaTransport = "websocket" | "livekit";

export interface SpeechKitVoiceCapabilities {
  dictation: boolean;
  assist: boolean;
  voice_agent: boolean;
  wakeword_local: boolean;
  tts: boolean;
  barge_in: boolean;
  local_pairing: boolean;
}

export interface SpeechKitVoiceSessionContext {
  session_id?: string;
  ai_session_id?: string;
  persona_id?: string;
  locale?: string;
  room_id?: string;
  device_id?: string;
}

export interface SpeechKitVoiceProviderContext {
  provider_id?: string;
  provider_kind?: SpeechKitProviderKind;
  media_transport?: SpeechKitMediaTransport;
}

export interface SpeechKitVoiceSurfaceContract {
  version: typeof SPEECHKIT_VOICE_SURFACE_VERSION;
  surface: SpeechKitVoiceSurface;
  account_id?: string;
  user_id?: string;
  org_id?: string;
  local_instance_id?: string;
  hosted_instance_id?: string;
  mode: SpeechKitVoiceMode;
  capture_policy: SpeechKitCapturePolicy;
  transport: SpeechKitTransport;
  capabilities: SpeechKitVoiceCapabilities;
  session: SpeechKitVoiceSessionContext;
  provider: SpeechKitVoiceProviderContext;
}

export interface SpeechKitVoiceDenial {
  error_code: string;
  reason_code: string;
  capability: string;
  provider_id?: string;
  required_features: string[];
  missing_features: string[];
  retryable: boolean;
  /** Short machine-friendly hint on how to unblock the denied capability (additive, optional). */
  remediation?: string;
  user_guidance: {
    title: string;
    body: string;
    next_steps: string[];
  };
  support_context?: {
    feature_source?: string;
    cost_bearing?: boolean;
  };
  /** Gateway/server correlation id for support and tracing (additive, optional). */
  request_id?: string;
}

export interface SpeechKitVoiceEvent {
  type: SpeechKitVoiceEventType;
  surface: SpeechKitVoiceSurface;
  mode: SpeechKitVoiceMode;
  session_id?: string;
  ai_session_id?: string;
  capture_policy?: SpeechKitCapturePolicy;
  transport?: SpeechKitTransport;
  text?: string;
  speak_text?: string;
  input_text?: string;
  output_text?: string;
  final?: boolean;
  reason_code?: string;
  error?: SpeechKitVoiceDenial;
  before_side_effects?: string[];
}

export type SpeechKitVoiceSessionStatus =
  | "idle"
  | "capturing"
  | "processing"
  | "speaking"
  | "cancelled"
  | "denied";

export interface SpeechKitVoiceSessionState {
  surface: SpeechKitVoiceSurface;
  mode: SpeechKitVoiceMode;
  // The explicit `| undefined` on the optional fields keeps the canonical
  // reducer assignments legal under this workspace's exactOptionalPropertyTypes
  // without changing the runtime shape.
  session_id?: string | undefined;
  ai_session_id?: string | undefined;
  status: SpeechKitVoiceSessionStatus;
  transcript_draft: string;
  transcript_final: string;
  spoken_response: string;
  denial?: SpeechKitVoiceDenial | undefined;
  reason_code?: string | undefined;
  barge_in: boolean;
  last_event_type?: SpeechKitVoiceEventType | undefined;
  events: SpeechKitVoiceEvent[];
}

export function isSpeechKitVoiceEventType(value: string): value is SpeechKitVoiceEventType {
  return (SPEECHKIT_VOICE_EVENT_TYPES as readonly string[]).includes(value);
}

export function isSpeechKitVoiceMode(value: string): value is SpeechKitVoiceMode {
  return (SPEECHKIT_VOICE_MODES as readonly string[]).includes(value);
}

export function createSpeechKitVoiceSessionState(
  contract: Pick<SpeechKitVoiceSurfaceContract, "surface" | "mode" | "session">
): SpeechKitVoiceSessionState {
  return {
    surface: contract.surface,
    mode: contract.mode,
    session_id: contract.session.session_id,
    ai_session_id: contract.session.ai_session_id,
    status: "idle",
    transcript_draft: "",
    transcript_final: "",
    spoken_response: "",
    barge_in: false,
    events: []
  };
}

export function reduceSpeechKitVoiceEvent(
  state: SpeechKitVoiceSessionState,
  event: SpeechKitVoiceEvent
): SpeechKitVoiceSessionState {
  const next: SpeechKitVoiceSessionState = {
    ...state,
    surface: event.surface,
    mode: event.mode,
    session_id: event.session_id ?? state.session_id,
    ai_session_id: event.ai_session_id ?? state.ai_session_id,
    last_event_type: event.type,
    events: [...state.events, event]
  };

  switch (event.type) {
    case "voice.capture_started":
      return { ...next, status: "capturing", reason_code: undefined, denial: undefined, barge_in: false };
    case "voice.capture_stopped":
      return { ...next, status: "processing" };
    case "voice.transcript_draft":
      return { ...next, status: "capturing", transcript_draft: event.text ?? state.transcript_draft };
    case "voice.transcript_final":
      return {
        ...next,
        status: "processing",
        transcript_draft: "",
        transcript_final: event.text ?? state.transcript_final
      };
    case "voice.assist_result":
    case "voice.agent_turn":
      return {
        ...next,
        status: event.speak_text || event.output_text ? "speaking" : "processing",
        spoken_response: event.speak_text ?? event.output_text ?? state.spoken_response
      };
    case "voice.tts_started":
      return { ...next, status: "speaking", spoken_response: event.speak_text ?? state.spoken_response };
    case "voice.tts_finished":
      return { ...next, status: "idle" };
    case "voice.cancelled":
      return { ...next, status: "cancelled", reason_code: event.reason_code };
    case "voice.barge_in":
      return { ...next, status: "capturing", barge_in: true, reason_code: event.reason_code };
    case "voice.denied":
      return {
        ...next,
        status: "denied",
        denial: event.error,
        reason_code: event.error?.reason_code ?? event.reason_code
      };
  }
}
