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
  checkMicrophonePermission,
  microphoneReasonFromError,
  releaseMediaStream
} from "../core/permission.js";
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

/**
 * Denial envelope with a canonical reason code. Guidance stays generic: raw
 * error messages (which can carry server URLs) go to `onDiagnostic` only;
 * the kit localizes the user-facing line from `reason_code`.
 */
function denialFromError(code: string, retryable: boolean): SpeechKitVoiceDenial {
  const microphone = code.startsWith("microphone_");
  return {
    error_code: code,
    reason_code: code,
    capability: "speechkit.voiceagent.live",
    required_features: [],
    missing_features: [],
    retryable,
    user_guidance: {
      title: microphone ? "Microphone is not available" : "Live voice session failed",
      body: microphone
        ? "The microphone could not be opened, so no voice session was started."
        : "The voice session could not be started or was interrupted.",
      next_steps: microphone
        ? ["Allow or connect a microphone, then try again."]
        : retryable
          ? ["Check the connection and try again."]
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
  interface Attempt {
    abort: AbortController;
    stream?: MediaStream;
    session?: BrowserSession;
    detachMic?: () => void;
    disposeMicMeter?: () => void;
    userBuffer: string;
    agentBuffer: string;
    interrupting: boolean;
    awaitingInterruptAck: boolean;
  }
  let attempt: Attempt | null = null;

  const isCurrent = (current: Attempt): boolean =>
    !disposed && attempt === current && !current.abort.signal.aborted;

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
    const closing = attempt;
    // Fence callbacks before aborting: the transport may close synchronously.
    attempt = null;
    if (!closing) return;
    closing.abort.abort();
    try { closing.detachMic?.(); } catch { /* release the other media resources */ }
    releaseMediaStream(closing.stream);
    try { closing.disposeMicMeter?.(); } catch { /* continue transport cleanup */ }
    try { closing.session?.flushPlayback(); } catch { /* playback already gone */ }
    try { closing.session?.close(); } catch { /* socket already gone */ }
    emitLevel(0, "input");
    emitLevel(0, "output");
  }

  async function startSession(current: Attempt): Promise<void> {
    if (!isCurrent(current)) return;
    // Robustness: never mint a server session (ticket, provider cost) for a
    // microphone the browser already blocks or cannot provide.
    if (!options.getAudioStream) {
      const permission = await checkMicrophonePermission();
      if (!isCurrent(current)) return;
      if (permission === "denied" || permission === "unsupported") {
        const code =
          permission === "denied" ? "microphone_permission_denied" : "microphone_unsupported";
        diag(`microphone ${permission}`);
        cleanup();
        emit({ type: "voice.denied", error: denialFromError(code, permission === "denied") });
        return;
      }
    }
    diag("requesting microphone…");
    let stream: MediaStream;
    try {
      stream = await (options.getAudioStream ?? defaultAudioStream)();
    } catch (err) {
      if (!isCurrent(current)) return;
      cleanup();
      diag(`microphone failed: ${(err as Error)?.message ?? String(err)}`);
      emit({
        type: "voice.denied",
        error: denialFromError(microphoneReasonFromError(err), true)
      });
      return;
    }
    // getUserMedia cannot be aborted. A late grant belongs to the old attempt.
    if (!isCurrent(current)) {
      releaseMediaStream(stream);
      return;
    }
    current.stream = stream;

    diag(`minting session at ${options.serverUrl}…`);
    try {
      const opened = await openBrowserSession({
        serverUrl: options.serverUrl,
        signal: current.abort.signal,
        ...(options.token !== undefined ? { token: options.token } : {}),
        ...(options.basePath !== undefined ? { basePath: options.basePath } : {}),
        start: options.start ?? {},
        onPlaybackLevel: (level) => { if (isCurrent(current)) emitLevel(level, "output"); },
        hooks: {
          onState: (agentState) => { if (isCurrent(current)) diag(`agent state: ${agentState}`); },
          onUserTranscript: (text, done) => {
            if (!isCurrent(current)) return;
            current.userBuffer = accumulateTranscript(current.userBuffer, text);
            if (done) {
              emit({ type: "voice.transcript_final", text: current.userBuffer, final: true });
              current.userBuffer = "";
            } else {
              emit({ type: "voice.transcript_draft", text: current.userBuffer });
            }
          },
          onAgentTranscript: (text, done) => {
            if (!isCurrent(current)) return;
            current.agentBuffer = accumulateTranscript(current.agentBuffer, text);
            emit({ type: "voice.agent_turn", output_text: current.agentBuffer, final: done });
            if (done) current.agentBuffer = "";
          },
          onAudio: (chunk) => { if (isCurrent(current)) current.session?.playChunk(chunk); },
          onInterrupted: (frame) => {
            if (!isCurrent(current)) return;
            current.session?.flushPlayback();
            current.agentBuffer = "";
            // cancel() calls this hook synchronously, then the wire ACK calls
            // it again. Both flush audio; the user action is emitted once below.
            if (current.interrupting) return;
            if (frame.provider_metadata?.reason === "client_cancel") {
              current.awaitingInterruptAck = false;
              return;
            }
            emit({ type: "voice.barge_in", reason_code: "user_spoke_during_tts" });
          },
          onError: (err) => {
            if (!isCurrent(current)) return;
            const message = err instanceof Error ? err.message : `${err.code}: ${err.message}`;
            diag(`error: ${message}`);
            if (!(err instanceof Error) && err.fatal !== true) {
              // A failed turn does not end the admitted microphone/session.
              current.userBuffer = "";
              current.agentBuffer = "";
              current.session?.flushPlayback();
              emit({ type: "voice.denied", error: denialFromError("session_error", true) });
              if (isCurrent(current)) emit({ type: "voice.capture_started" });
              return;
            }
            cleanup();
            emit({ type: "voice.denied", error: denialFromError("session_error", true) });
          },
          onClose: (reason) => {
            if (!isCurrent(current)) return;
            diag(`session closed: ${reason}`);
            cleanup();
            emit({ type: "voice.tts_finished" });
          }
        }
      });
      if (!isCurrent(current)) {
        opened.close();
        return;
      }
      current.session = opened;
      const detachMic = opened.attachMicrophone(stream);
      // A host audio adapter may fail synchronously while attaching capture.
      if (!isCurrent(current)) {
        detachMic();
        return;
      }
      current.detachMic = detachMic;
      current.disposeMicMeter = createMicLevelMeter(stream, (level) => {
        if (isCurrent(current)) emitLevel(level, "input");
      });
      diag(`session ${opened.ticket.session_id} live`);
      emit({ type: "voice.capture_started" });
    } catch (err) {
      if (!isCurrent(current)) return;
      cleanup();
      diag(`connect failed: ${(err as Error).message}`);
      emit({
        type: "voice.denied",
        error: denialFromError("session_connect_failed", true)
      });
      return;
    }
  }

  return {
    contract,
    start(_mode?: SpeechKitVoiceMode) {
      if (attempt || disposed) return;
      const current: Attempt = {
        abort: new AbortController(), userBuffer: "", agentBuffer: "",
        interrupting: false, awaitingInterruptAck: false
      };
      attempt = current;
      // Offer Stop throughout permission/mint setup, without claiming capture yet.
      state = { ...createSpeechKitVoiceSessionState(contract), status: "processing" };
      for (const listener of listeners) listener(state);
      return startSession(current);
    },
    stop() {
      if (!attempt) return;
      cleanup();
      emit({ type: "voice.capture_stopped" });
      emit({ type: "voice.tts_finished" });
    },
    cancel() {
      cleanup();
      emit({ type: "voice.cancelled", reason_code: "user_cancelled" });
    },
    interrupt() {
      const current = attempt;
      const session = current?.session;
      if (!current || !session || current.awaitingInterruptAck) return;
      // The client flushes via onInterrupted, cancels the provider reply and
      // mutes late downlink audio until the server acknowledges interruption.
      current.awaitingInterruptAck = true;
      current.interrupting = true;
      try { session.session.cancel(); }
      finally { current.interrupting = false; }
      if (isCurrent(current)) emit({ type: "voice.barge_in", reason_code: "user_interrupt_button" });
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
      disposed = true;
      cleanup();
      listeners.clear();
      levelListeners.clear();
    }
  };
}
