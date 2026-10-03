import { CLIENT_SAMPLE_RATE, SERVER_SAMPLE_RATE, type SessionTicket } from "./protocol.js";
import {
  VoiceAgentSession,
  VoiceAgentClientError,
  MAX_PLAYBACK_BYTES,
  throwIfAborted,
  deriveWsUrl,
  mintSessionTicket,
  ticketSubprotocol,
  type SessionOptions,
  type WireSocket,
} from "./session.js";

export interface BrowserSocket extends WireSocket {
  binaryType: string;
}

export interface BrowserOpenOptions<TSocket extends BrowserSocket = WebSocket> extends SessionOptions {
  serverUrl: string;
  token?: string;
  /**
   * Path prefix in front of the versioned API route. Defaults to `/v1`
   * (direct server access). Gateway consumers pass e.g.
   * `serverUrl: "https://api.kombify.io"` + `basePath: "/v1/speechkit"`.
   */
  basePath?: string;
  /** REST body for POST /v1/voiceagent/sessions. */
  ticket?: Record<string, unknown>;
  /** Pre-minted ticket; skips the REST call when provided. */
  presetTicket?: SessionTicket;
  /**
   * Rewrite the WebSocket URL before connecting. The server-returned
   * `ws_url` points at the origin; gateway consumers rewrite it to the
   * gateway host, e.g.
   * `wss://api.kombify.io/v1/speechkit/voiceagent/sessions/{id}/ws`. The
   * upgrade ticket stays in the `Sec-WebSocket-Protocol` subprotocol
   * either way.
   */
  resolveWsUrl?: (ticket: SessionTicket) => string;
  /** Host socket factory; native WebSocket is used by default. */
  createWebSocket?: (url: string, protocols: string[]) => TSocket;
  /** Host audio context factory, shared by default capture and playback. */
  createAudioContext?: () => AudioContext;
  /** Prepared host playback adapter. Session cleanup calls dispose once. */
  playback?: BrowserPlayback;
  /**
   * Optional level tap for chunks scheduled via {@link BrowserSession.playChunk}:
   * called with the RMS (0..1) of each chunk before it is scheduled, and with
   * 0 once the queue drains or is flushed. Feed this to output visualizers.
   */
  onPlaybackLevel?: (level: number) => void;
}

export interface BrowserSession<TSocket extends BrowserSocket = WebSocket> {
  session: VoiceAgentSession;
  socket: TSocket;
  ticket: SessionTicket;
  /**
   * Wire a captured MediaStream into the session. Resamples the input to
   * 16 kHz signed-int16 mono and streams it as binary frames. Uses an
   * AudioWorklet when available and falls back to a ScriptProcessorNode.
   * Returns a function that stops capture and releases the audio context.
   */
  attachMicrophone(stream: MediaStream): () => void;
  /**
   * Plays an incoming audio chunk (24 kHz S16 LE mono PCM ArrayBuffer)
   * through a shared {@link AudioContext}. Call this from
   * `hooks.onAudio` for a default speaker pipeline.
   */
  playChunk(chunk: ArrayBuffer): void;
  /**
   * Immediately drops all queued agent audio (stops scheduled sources and
   * resets the schedule head). Call on barge-in — typically from
   * `hooks.onInterrupted` — so stale agent speech does not keep playing
   * over the user.
   */
  flushPlayback(): void;
  /** Call directly from a click/tap to resume a suspended speaker context. */
  resumeAudio(): Promise<void>;
  close(): void;
}

/**
 * Browser entry point. Mints a ticket if not supplied, opens a
 * WebSocket authenticated via the `ticket.<ticket>` subprotocol, and
 * returns a {@link BrowserSession} ready to attach mic + playback.
 */
export function openBrowserSession<TSocket extends BrowserSocket = WebSocket>(options: BrowserOpenOptions<TSocket>): Promise<BrowserSession<TSocket>>;
export async function openBrowserSession(options: BrowserOpenOptions<BrowserSocket>): Promise<BrowserSession<BrowserSocket>> {
  throwIfAborted(options.signal);
  const captures = new Set<() => void>();
  let session: VoiceAgentSession | undefined;
  const playback = options.playback ?? createPlayback(options.onPlaybackLevel, code => session?.fail(code), options.createAudioContext);
  let cleaned = false;
  const cleanup = () => {
    if (cleaned) return;
    cleaned = true;
    for (const stop of captures) { try { stop(); } catch { /* release other captures */ } }
    captures.clear();
    try { playback.dispose(); } catch { /* host adapter must not prevent session cleanup */ }
  };
  try {
    // Invoke resume before minting awaits so the initiating gesture is active.
    // A host can supply playback prepared before its own authentication awaits.
    if (options.playback || options.createAudioContext || typeof AudioContext === "function") void playback.resume().catch(() => undefined);
    const ticket =
      options.presetTicket ??
      (await mintSessionTicket({
        serverUrl: options.serverUrl,
        ...(options.token !== undefined ? { token: options.token } : {}),
        ...(options.ticket !== undefined ? { body: options.ticket } : {}),
        ...(options.basePath !== undefined ? { basePath: options.basePath } : {}),
        ...(options.signal !== undefined ? { signal: options.signal } : {}),
      }));
    throwIfAborted(options.signal);
    const url = options.resolveWsUrl
      ? options.resolveWsUrl(ticket)
      : deriveWsUrl(options.serverUrl, ticket, options.basePath);
    const socket = options.createWebSocket
      ? options.createWebSocket(url, [ticketSubprotocol(ticket)])
      : new WebSocket(url, [ticketSubprotocol(ticket)]);
    socket.binaryType = "arraybuffer";

    const sessionOptions: SessionOptions = {
      start: options.start,
      ...(options.connectTimeoutMs !== undefined ? { connectTimeoutMs: options.connectTimeoutMs } : {}),
      ...(options.readyTimeoutMs !== undefined ? { readyTimeoutMs: options.readyTimeoutMs } : {}),
      ...(options.maxOutboundBytes !== undefined ? { maxOutboundBytes: options.maxOutboundBytes } : {}),
      ...(options.signal !== undefined ? { signal: options.signal } : {}),
      hooks: {
        ...options.hooks,
        onInterrupted: frame => {
          playback.flush();
          options.hooks?.onInterrupted?.(frame);
        },
        onClose: reason => {
          cleanup();
          options.hooks?.onClose?.(reason);
        },
      },
      ...(options.tools !== undefined ? { tools: options.tools } : {}),
    };
    const activeSession = new VoiceAgentSession(socket, sessionOptions);
    session = activeSession;

    const handle: BrowserSession<BrowserSocket> = {
      session: activeSession,
      socket,
      ticket,
      attachMicrophone: stream => {
        if (activeSession.isClosed) {
          stream.getTracks().forEach(track => track.stop());
          return () => undefined;
        }
        let stopCapture: () => void;
        try { stopCapture = attachMicrophone(activeSession, stream, options.createAudioContext); }
        catch {
          stream.getTracks().forEach(track => { try { track.stop(); } catch { /* release other tracks */ } });
          activeSession.fail("audio_capture_failed");
          return () => undefined;
        }
        // A host factory can cancel synchronously while creating its context.
        // Terminal cleanup may have run before this capture was registered.
        if (activeSession.isClosed) {
          stopCapture();
          return () => undefined;
        }
        const stop = () => { captures.delete(stop); stopCapture(); };
        captures.add(stop);
        return stop;
      },
      playChunk: chunk => { if (activeSession.acceptsAudio) playback.play(chunk); },
      flushPlayback: () => playback.flush(),
      resumeAudio: () => playback.resume(),
      close: () => {
        cleanup();
        activeSession.close();
      },
    };
    await activeSession.ready;
    if (activeSession.isClosed) throw new VoiceAgentClientError("ws_setup_failed");
    return handle;
  } catch (error) {
    cleanup();
    session?.close();
    throw error instanceof VoiceAgentClientError ? error : new VoiceAgentClientError("ws_setup_failed");
  }
}

/**
 * AudioWorklet processor that forwards mono Float32 input chunks to the
 * main thread. Inlined as a Blob module so the package stays
 * self-contained (no separate worklet asset to host).
 */
const CAPTURE_WORKLET_SOURCE = `
registerProcessor("speechkit-capture", class extends AudioWorkletProcessor {
  process(inputs) {
    const channel = inputs[0] && inputs[0][0];
    if (channel && channel.length > 0) {
      const copy = new Float32Array(channel.length);
      copy.set(channel);
      this.port.postMessage(copy, [copy.buffer]);
    }
    return true;
  }
});
`;

function attachMicrophone(session: VoiceAgentSession, stream: MediaStream, createAudioContext?: () => AudioContext): () => void {
  const audioContext = createAudioContext ? createAudioContext() : new AudioContext();
  let source: MediaStreamAudioSourceNode;
  try { source = audioContext.createMediaStreamSource(stream); }
  catch {
    try { void audioContext.close().catch(() => undefined); } catch { /* already closed */ }
    throw new VoiceAgentClientError("audio_capture_failed");
  }
  const sourceRate = audioContext.sampleRate;
  let leftover: Float32Array | null = null;
  let stopped = false;
  let disposeGraph: (() => void) | null = null;
  const stop = () => {
    if (stopped) return;
    stopped = true;
    try { disposeGraph?.(); } catch { /* continue releasing capture */ }
    disposeGraph = null;
    try { source.disconnect(); } catch { /* already disconnected */ }
    try { void audioContext.close().catch(() => undefined); } catch { /* already closed */ }
    stream.getTracks().forEach(track => { try { track.stop(); } catch { /* release other tracks */ } });
  };

  const push = (channelData: Float32Array): void => {
    if (stopped || session.isClosed) return;
    const merged = leftover ? concatFloat32(leftover, channelData) : new Float32Array(channelData);
    const { pcm16, remaining } = downsampleToInt16(merged, sourceRate, CLIENT_SAMPLE_RATE);
    leftover = remaining;
    if (pcm16.length > 0) {
      session.sendAudioChunk(pcm16);
    }
  };

  // Worklet setup is async; the returned stop function tears down
  // whichever graph finished wiring up.
  void (async () => {
    await audioContext.resume();
    if (stopped) return;
    const graph = await createCaptureGraph(audioContext, source, push, () => stopped);
    if (stopped) {
      graph.dispose();
      return;
    }
    disposeGraph = graph.dispose;
  })().catch(() => { stop(); if (!session.isClosed) session.fail("audio_capture_failed"); });

  return stop;
}

interface CaptureGraph {
  dispose(): void;
}

async function createCaptureGraph(
  audioContext: AudioContext,
  source: MediaStreamAudioSourceNode,
  push: (channelData: Float32Array) => void,
  stopped: () => boolean,
): Promise<CaptureGraph> {
  // Connect through a muted sink so the graph stays alive; otherwise
  // some browsers garbage-collect it after a few hundred ms.
  const sink = audioContext.createGain();
  sink.gain.value = 0;
  sink.connect(audioContext.destination);

  if (typeof AudioWorkletNode === "function" && audioContext.audioWorklet) {
    try {
      const blob = new Blob([CAPTURE_WORKLET_SOURCE], { type: "application/javascript" });
      const moduleUrl = URL.createObjectURL(blob);
      try {
        await audioContext.audioWorklet.addModule(moduleUrl);
      } finally {
        URL.revokeObjectURL(moduleUrl);
      }
      if (stopped()) { sink.disconnect(); return { dispose: () => undefined }; }
      const worklet = new AudioWorkletNode(audioContext, "speechkit-capture", {
        numberOfInputs: 1,
        numberOfOutputs: 1,
        channelCount: 1,
      });
      worklet.port.onmessage = (event: MessageEvent) => push(event.data as Float32Array);
      source.connect(worklet);
      worklet.connect(sink);
      return {
        dispose: () => {
          worklet.port.onmessage = null;
          worklet.disconnect();
          sink.disconnect();
        },
      };
    } catch {
      // Strict CSPs can block Blob worklet modules; fall through to the
      // ScriptProcessor path below.
    }
  }

  if (stopped()) { sink.disconnect(); return { dispose: () => undefined }; }

  // Fallback: ScriptProcessorNode is deprecated but universally
  // available.
  const processor = audioContext.createScriptProcessor(4096, 1, 1);
  processor.onaudioprocess = (event: AudioProcessingEvent) => {
    push(event.inputBuffer.getChannelData(0));
  };
  source.connect(processor);
  processor.connect(sink);
  return {
    dispose: () => {
      processor.onaudioprocess = null;
      processor.disconnect();
      sink.disconnect();
    },
  };
}

function concatFloat32(a: Float32Array, b: Float32Array): Float32Array {
  const out = new Float32Array(a.length + b.length);
  out.set(a, 0);
  out.set(b, a.length);
  return out;
}

/**
 * Linear-interpolation downsample from `sourceRate` to `targetRate` and
 * convert to signed-int16 PCM. Returns leftover samples that did not
 * align with a target tick so the next call can continue smoothly.
 */
export function downsampleToInt16(
  input: Float32Array,
  sourceRate: number,
  targetRate: number,
): { pcm16: Int16Array; remaining: Float32Array } {
  if (sourceRate === targetRate) {
    return { pcm16: floatToInt16(input), remaining: new Float32Array(0) };
  }
  const ratio = sourceRate / targetRate;
  const outputLength = Math.floor(input.length / ratio);
  const pcm16 = new Int16Array(outputLength);
  for (let i = 0; i < outputLength; i++) {
    const idx = i * ratio;
    const lo = Math.floor(idx);
    const hi = Math.min(lo + 1, input.length - 1);
    const frac = idx - lo;
    const sample = (input[lo] ?? 0) * (1 - frac) + (input[hi] ?? 0) * frac;
    pcm16[i] = clampInt16(sample * 0x7fff);
  }
  const consumed = Math.floor(outputLength * ratio);
  return {
    pcm16,
    remaining: input.slice(consumed),
  };
}

function floatToInt16(input: Float32Array): Int16Array {
  const out = new Int16Array(input.length);
  for (let i = 0; i < input.length; i++) {
    out[i] = clampInt16((input[i] ?? 0) * 0x7fff);
  }
  return out;
}

function clampInt16(value: number): number {
  if (value > 0x7fff) return 0x7fff;
  if (value < -0x8000) return -0x8000;
  return Math.round(value);
}

export interface BrowserPlayback {
  play(chunk: ArrayBuffer): void;
  flush(): void;
  dispose(): void;
  resume(): Promise<void>;
}

function createPlayback(onLevel: ((level: number) => void) | undefined, onFailure: (code: string) => void, createAudioContext?: () => AudioContext): BrowserPlayback {
  let context: AudioContext | null = null;
  let nextStartTime = 0;
  const active = new Map<AudioBufferSourceNode, number>();
  let bufferedBytes = 0;
  let disposed = false;
  const emitLevel = (level: number) => { try { onLevel?.(level); } catch { /* advisory host callback */ } };

  function ensureContext(): AudioContext {
    if (!context) {
      context = createAudioContext ? createAudioContext() : new AudioContext({ sampleRate: SERVER_SAMPLE_RATE });
      nextStartTime = context.currentTime;
    }
    return context;
  }

  return {
    async resume() {
      if (disposed) throw new VoiceAgentClientError("session_closed");
      try { await ensureContext().resume(); }
      catch { throw new VoiceAgentClientError("audio_resume_required"); }
    },
    play(chunk) {
      if (disposed) return;
      if (!chunk.byteLength || chunk.byteLength % 2 || bufferedBytes + chunk.byteLength > MAX_PLAYBACK_BYTES || active.size >= 64) {
        onFailure("voice_buffer_overflow"); return;
      }
      try {
        const ctx = ensureContext();
        if (ctx.state !== "running") { onFailure("audio_resume_required"); return; }
        const samples = new Int16Array(chunk);
        const buffer = ctx.createBuffer(1, samples.length, SERVER_SAMPLE_RATE);
        const channel = buffer.getChannelData(0);
        let sumSquares = 0;
        for (let i = 0; i < samples.length; i++) {
          const value = (samples[i] ?? 0) / 0x7fff;
          channel[i] = value;
          sumSquares += value * value;
        }
        emitLevel(Math.min(1, Math.sqrt(sumSquares / Math.max(1, samples.length)) * 3));
        const source = ctx.createBufferSource();
        source.buffer = buffer;
        source.connect(ctx.destination);
        active.set(source, chunk.byteLength);
        bufferedBytes += chunk.byteLength;
        source.onended = () => {
          bufferedBytes -= active.get(source) ?? 0;
          active.delete(source);
          source.disconnect();
          if (active.size === 0) emitLevel(0);
        };
        const startAt = Math.max(ctx.currentTime, nextStartTime);
        source.start(startAt);
        nextStartTime = startAt + buffer.duration;
      } catch { this.flush(); onFailure("audio_playback_failed"); }
    },
    flush() {
      for (const source of active.keys()) {
        source.onended = null;
        try {
          source.stop();
        } catch {
          /* already stopped */
        }
        try { source.disconnect(); } catch { /* already disconnected */ }
      }
      active.clear();
      bufferedBytes = 0;
      if (context) nextStartTime = context.currentTime;
      emitLevel(0);
    },
    dispose() {
      if (disposed) return;
      disposed = true;
      this.flush();
      if (context) {
        try { void context.close().catch(() => undefined); } catch { /* already closed */ }
        context = null;
      }
    },
  };
}

export type { SessionHooks, ToolContext } from "./session.js";
export { VoiceAgentSession, VoiceAgentClientError } from "./session.js";
