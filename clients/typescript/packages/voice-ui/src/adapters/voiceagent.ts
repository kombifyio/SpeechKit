/**
 * Official OSS adapter over the public SpeechKit voice-agent client — the
 * adapter the {@link VoiceUiController} docstring promises for OSS/self-host
 * consumers: real microphone → SpeechKit server WebSocket → agent audio back,
 * translated into the canonical `speechkit.voice_surface.v1` event stream the
 * kit renders.
 *
 * Ships as the separate subpath `@kombifyio/speechkit-voice-ui/voiceagent-adapter`
 * so the main entry stays dependency-free; using it requires the optional peer
 * dependency `@kombifyio/speechkit-voiceagent-client`.
 */

import {
  openBrowserSession,
  type BrowserSession
} from "@kombifyio/speechkit-voiceagent-client/browser";
import { accumulateTranscript, type StartFrame } from "@kombifyio/speechkit-voiceagent-client/protocol";
import type { VoiceUiController } from "../core/controller.js";
import {
  createSpeechKitVoiceSessionState,
  reduceSpeechKitVoiceEvent,
  type SpeechKitVoiceDenial,
  type SpeechKitVoiceEvent,
  type SpeechKitVoiceMode,
  type SpeechKitVoiceSessionState,
  type SpeechKitVoiceSurface,
  type SpeechKitVoiceSurfaceContract
} from "../core/voice-surface.js";

export interface VoiceAgentUiControllerOptions {
  /** SpeechKit server origin, e.g. `http://localhost:8080`. */
  serverUrl: string;
  /** Bearer token for `POST /v1/voiceagent/sessions` (omit for auth_mode=none). */
  token?: string;
  /** API path prefix, defaults to `/v1` (gateway consumers pass e.g. `/v1/speechkit`). */
  basePath?: string;
  /** Surface reported in the contract and on every event. Default `floating_panel`. */
  surface?: SpeechKitVoiceSurface;
  /** Session start parameters (provider, persona_id, locale, voice, model, …). */
  start?: Omit<StartFrame, "type">;
  /**
   * Microphone acquisition override. Defaults to
   * `getUserMedia({audio: {echoCancellation, noiseSuppression, mono}})`.
   * Hosts that manage capture consent themselves inject their stream here.
   */
  getAudioStream?: () => Promise<MediaStream>;
  /** Out-of-band connection diagnostics (never part of the event contract). */
  onDiagnostic?: (message: string) => void;
}

/** {@link VoiceUiController} with the optional members guaranteed present. */
export interface VoiceAgentUiController extends VoiceUiController {
  interrupt(): void;
  subscribeLevel(listener: (level: number, source: "input" | "output") => void): () => void;
  /** Tear down socket, mic, and audio graphs. The controller is unusable afterwards. */
  dispose(): void;
}

function contractFor(options: VoiceAgentUiControllerOptions): SpeechKitVoiceSurfaceContract {
  return {
    version: "speechkit.voice_surface.v1",
    surface: options.surface ?? "floating_panel",
    mode: "voice_agent",
    capture_policy: "push_to_talk",
    transport: "voiceagent_ws_ticket",
    capabilities: {
      dictation: false,
      assist: false,
      voice_agent: true,
      wakeword_local: false,
      tts: true,
      barge_in: true,
      local_pairing: false
    },
    session: {},
    provider: options.start?.provider !== undefined ? { provider_id: options.start.provider } : {}
  };
}

function denialFromError(code: string, message: string, retryable: boolean): SpeechKitVoiceDenial {
  return {
    error_code: code,
    reason_code: code,
    capability: "speechkit.voiceagent.live",
    required_features: [],
    missing_features: [],
    retryable,
    user_guidance: {
      title: "Live voice session failed",
      body: message,
      next_steps: retryable
        ? ["Check the server and try again."]
        : ["Check the voice-agent configuration."]
    }
  };
}

function defaultAudioStream(): Promise<MediaStream> {
  return navigator.mediaDevices.getUserMedia({
    audio: { echoCancellation: true, noiseSuppression: true, channelCount: 1 }
  });
}

/** RMS meter over the raw microphone stream (~20 Hz). No-op without Web Audio. */
function createMicLevelMeter(stream: MediaStream, onLevel: (level: number) => void): () => void {
  if (typeof AudioContext === "undefined") return () => {};
  const context = new AudioContext();
  const analyser = context.createAnalyser();
  analyser.fftSize = 1024;
  context.createMediaStreamSource(stream).connect(analyser);
  const data = new Float32Array(analyser.fftSize);
  const timer = setInterval(() => {
    analyser.getFloatTimeDomainData(data);
    let sumSquares = 0;
    for (let i = 0; i < data.length; i++) sumSquares += (data[i] ?? 0) ** 2;
    onLevel(Math.min(1, Math.sqrt(sumSquares / data.length) * 4));
  }, 50);
  return () => {
    clearInterval(timer);
    onLevel(0);
    void context.close();
  };
}

/**
 * Creates a {@link VoiceAgentUiController} bound to a SpeechKit server.
 * `start()` mints a session ticket, opens the WebSocket, and attaches the
 * microphone; every session hook is reduced through the canonical
 * voice_surface reducer, so kit elements render live sessions exactly like
 * host-fed ones.
 */
export function createVoiceAgentUiController(
  options: VoiceAgentUiControllerOptions
): VoiceAgentUiController {
  const contract = contractFor(options);
  let state = createSpeechKitVoiceSessionState(contract);
  const listeners = new Set<(state: SpeechKitVoiceSessionState) => void>();
  const levelListeners = new Set<(level: number, source: "input" | "output") => void>();
  let disposed = false;
  let active = false;
  let session: BrowserSession | null = null;
  let detachMic: (() => void) | null = null;
  let disposeMicMeter: (() => void) | null = null;
  let userBuffer = "";
  let agentBuffer = "";

  const diag = (message: string): void => options.onDiagnostic?.(message);

  const emitLevel = (level: number, source: "input" | "output"): void => {
    for (const listener of levelListeners) listener(level, source);
  };

  function emit(event: Partial<SpeechKitVoiceEvent> & { type: SpeechKitVoiceEvent["type"] }): void {
    if (disposed) return;
    const full: SpeechKitVoiceEvent = {
      surface: contract.surface,
      mode: contract.mode,
      ...event
    };
    state = reduceSpeechKitVoiceEvent(state, full);
    for (const listener of listeners) listener(state);
  }

  function cleanup(): void {
    detachMic?.();
    detachMic = null;
    disposeMicMeter?.();
    disposeMicMeter = null;
    const closing = session;
    session = null;
    active = false;
    if (closing) {
      try {
        closing.flushPlayback();
        closing.close();
      } catch {
        /* socket already gone */
      }
    }
    emitLevel(0, "input");
    emitLevel(0, "output");
  }

  async function startSession(): Promise<void> {
    state = createSpeechKitVoiceSessionState(contract);
    userBuffer = "";
    agentBuffer = "";
    diag("requesting microphone…");
    let stream: MediaStream;
    try {
      stream = await (options.getAudioStream ?? defaultAudioStream)();
    } catch (err) {
      emit({
        type: "voice.denied",
        error: denialFromError("mic_permission_denied", (err as Error).message, true)
      });
      return;
    }

    diag(`minting session at ${options.serverUrl}…`);
    try {
      session = await openBrowserSession({
        serverUrl: options.serverUrl,
        ...(options.token !== undefined ? { token: options.token } : {}),
        ...(options.basePath !== undefined ? { basePath: options.basePath } : {}),
        start: options.start ?? {},
        onPlaybackLevel: (level) => emitLevel(level, "output"),
        hooks: {
          onState: (agentState) => diag(`agent state: ${agentState}`),
          onUserTranscript: (text, done) => {
            userBuffer = accumulateTranscript(userBuffer, text);
            if (done) {
              emit({ type: "voice.transcript_final", text: userBuffer, final: true });
              userBuffer = "";
            } else {
              emit({ type: "voice.transcript_draft", text: userBuffer });
            }
          },
          onAgentTranscript: (text, done) => {
            agentBuffer = accumulateTranscript(agentBuffer, text);
            emit({ type: "voice.agent_turn", output_text: agentBuffer, final: done });
            if (done) agentBuffer = "";
          },
          onAudio: (chunk) => session?.playChunk(chunk),
          onInterrupted: () => {
            session?.flushPlayback();
            emit({ type: "voice.barge_in", reason_code: "user_spoke_during_tts" });
          },
          onError: (err) => {
            const message = err instanceof Error ? err.message : `${err.code}: ${err.message}`;
            diag(`error: ${message}`);
            emit({ type: "voice.denied", error: denialFromError("session_error", message, true) });
          },
          onClose: (reason) => {
            diag(`session closed: ${reason}`);
            if (active) {
              cleanup();
              emit({ type: "voice.tts_finished" });
            }
          }
        }
      });
    } catch (err) {
      stream.getTracks().forEach((track) => track.stop());
      diag(`connect failed: ${(err as Error).message}`);
      emit({
        type: "voice.denied",
        error: denialFromError("session_connect_failed", (err as Error).message, true)
      });
      return;
    }

    detachMic = session.attachMicrophone(stream);
    disposeMicMeter = createMicLevelMeter(stream, (level) => emitLevel(level, "input"));
    active = true;
    diag(`session ${session.ticket.session_id} live`);
    emit({ type: "voice.capture_started" });
  }

  return {
    contract,
    start(_mode?: SpeechKitVoiceMode) {
      if (active || disposed) return;
      return startSession();
    },
    stop() {
      if (!active) return;
      emit({ type: "voice.capture_stopped" });
      cleanup();
      emit({ type: "voice.tts_finished" });
    },
    cancel() {
      cleanup();
      emit({ type: "voice.cancelled", reason_code: "user_cancelled" });
    },
    interrupt() {
      // Local barge-in affordance: drop queued playback and mark the turn.
      // Provider-driven barge-in additionally arrives via onInterrupted.
      session?.flushPlayback();
      emit({ type: "voice.barge_in", reason_code: "user_interrupt_button" });
    },
    subscribe(listener) {
      listeners.add(listener);
      listener(state);
      return () => listeners.delete(listener);
    },
    getState() {
      return state;
    },
    subscribeLevel(listener) {
      levelListeners.add(listener);
      return () => levelListeners.delete(listener);
    },
    dispose() {
      cleanup();
      disposed = true;
      listeners.clear();
      levelListeners.clear();
    }
  };
}
