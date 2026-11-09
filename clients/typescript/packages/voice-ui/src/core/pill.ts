import type { RecordingIndicatorState } from "./indicator.js";
import type { VoiceUiMessageId } from "../i18n/index.js";

/**
 * The in-Composer voice pill (spec §Voice pill, tokens.json → pill). Dictation
 * shows the elapsed recording time while listening; the Voice Agent shows its
 * phase instead. Replayed by `spec/fixtures/voice-ui-pill.v1.json`.
 */
export type VoicePillMode = "dictation" | "voice_agent";

/** Pill phases: the recording indicator states a running session can be in. */
export type VoicePillPhase = Exclude<RecordingIndicatorState, "idle">;

export const VOICE_PILL_PHASES: readonly VoicePillPhase[] = [
  "requesting",
  "listening",
  "processing",
  "speaking",
  "error",
];

export function isVoicePillPhase(value: unknown): value is VoicePillPhase {
  return typeof value === "string" && (VOICE_PILL_PHASES as readonly string[]).includes(value);
}

export interface VoicePillView {
  /** Recording indicator state that renders the dot and bars. */
  indicator: VoicePillPhase;
  /** Stable message ID of the phase label (visible or assistive-only). */
  label: VoiceUiMessageId;
  /** True when the visible text is the elapsed time instead of the label. */
  elapsed: boolean;
  /** True while the pill waits on setup or a result (`aria-busy`). */
  busy: boolean;
}

export function voicePillView(mode: VoicePillMode, phase: VoicePillPhase): VoicePillView {
  switch (phase) {
    case "requesting":
      return {
        indicator: phase,
        label: mode === "voice_agent" ? "sk.voice.agent.connecting" : "sk.voice.state.requesting",
        elapsed: false,
        busy: true,
      };
    case "listening":
      return {
        indicator: phase,
        label: "sk.voice.state.capturing",
        elapsed: mode === "dictation",
        busy: false,
      };
    case "processing":
      return { indicator: phase, label: "sk.voice.state.processing", elapsed: false, busy: true };
    case "speaking":
      return { indicator: phase, label: "sk.voice.state.speaking", elapsed: false, busy: false };
    case "error":
      return { indicator: phase, label: "sk.voice.state.denied", elapsed: false, busy: false };
  }
}

/** Elapsed recording time as `m:ss` (minutes keep counting past 59). */
export function formatVoicePillElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${minutes}:${seconds.toString().padStart(2, "0")}`;
}
