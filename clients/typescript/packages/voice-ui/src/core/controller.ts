import type {
  SpeechKitVoiceMode,
  SpeechKitVoiceSessionState,
  SpeechKitVoiceSessionStatus,
  SpeechKitVoiceSurfaceContract
} from "./voice-surface.js";

/**
 * Host-injected voice session controller. The kit renders state; the host owns
 * sessions, transports, entitlements, and audio. Structurally satisfied by
 * `createVoiceController` from `@kombify/ai-sdk/voice` on kombify surfaces and
 * by adapters over the public SpeechKit clients for OSS/self-host consumers.
 *
 * `subscribe` follows the plain store contract: the listener is invoked
 * synchronously with the current state and on every change.
 */
export interface VoiceUiController {
  readonly contract: SpeechKitVoiceSurfaceContract;
  start(mode?: SpeechKitVoiceMode): void | Promise<void>;
  stop(): void | Promise<void>;
  cancel(): void;
  /**
   * Barge-in: flush queued agent playback. Optional — the overlay hides the
   * tap-to-interrupt affordance when absent.
   */
  interrupt?(): void;
  subscribe(listener: (state: SpeechKitVoiceSessionState) => void): () => void;
  getState(): SpeechKitVoiceSessionState;
  /**
   * Audio level feed for the visualizer (0..1 RMS; input mic level or output
   * playback level). Optional — visualizers fall back to status-only motion.
   */
  subscribeLevel?(listener: (level: number, source: "input" | "output") => void): () => void;
}

export const VOICE_ACTIVE_STATUSES: ReadonlySet<SpeechKitVoiceSessionStatus> = new Set([
  "capturing",
  "processing",
  "speaking"
]);

export function isVoiceSessionActive(status: SpeechKitVoiceSessionStatus): boolean {
  return VOICE_ACTIVE_STATUSES.has(status);
}
