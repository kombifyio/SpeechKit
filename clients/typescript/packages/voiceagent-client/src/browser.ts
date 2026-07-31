import { CLIENT_SAMPLE_RATE, SERVER_SAMPLE_RATE, type SessionTicket } from "./protocol.js";
import {
  VoiceAgentSession,
  deriveWsUrl,
  mintSessionTicket,
  ticketSubprotocol,
  type SessionOptions,
} from "./session.js";

export interface BrowserOpenOptions extends SessionOptions {
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
}

export interface BrowserSession {
  session: VoiceAgentSession;
  socket: WebSocket;
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
  close(): void;
}

/**
 * Browser entry point. Mints a ticket if not supplied, opens a
 * WebSocket authenticated via the `ticket.<ticket>` subprotocol, and
 * returns a {@link BrowserSession} ready to attach mic + playback.
 */
export async function openBrowserSession(options: BrowserOpenOptions): Promise<BrowserSession> {
  const ticket =
    options.presetTicket ??
    (await mintSessionTicket({
      serverUrl: options.serverUrl,
      ...(options.token !== undefined ? { token: options.token } : {}),
      ...(options.ticket !== undefined ? { body: options.ticket } : {}),
      ...(options.basePath !== undefined ? { basePath: options.basePath } : {}),
    }));
  const url = options.resolveWsUrl
    ? options.resolveWsUrl(ticket)
    : deriveWsUrl(options.serverUrl, ticket, options.basePath);
  const socket = new WebSocket(url, [ticketSubprotocol(ticket)]);
  socket.binaryType = "arraybuffer";

  const sessionOptions: SessionOptions = {
    start: options.start,
    ...(options.hooks !== undefined ? { hooks: options.hooks } : {}),
    ...(options.tools !== undefined ? { tools: options.tools } : {}),
  };
  const session = new VoiceAgentSession(socket, sessionOptions);
  const playback = createPlayback();

  const handle: BrowserSession = {
    session,
    socket,
    ticket,
    attachMicrophone: (stream) => attachMicrophone(session, stream),
    playChunk: (chunk) => playback.play(chunk),
    close: () => {
      session.close();
      playback.dispose();
    },
  };
  return handle;
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

function attachMicrophone(session: VoiceAgentSession, stream: MediaStream): () => void {
  const audioContext = new AudioContext();
  const source = audioContext.createMediaStreamSource(stream);
  const sourceRate = audioContext.sampleRate;
  let leftover: Float32Array | null = null;
  let stopped = false;
  let disposeGraph: (() => void) | null = null;

  const push = (channelData: Float32Array): void => {
    if (stopped) return;
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
    const graph = await createCaptureGraph(audioContext, source, push);
    if (stopped) {
      graph.dispose();
      return;
    }
    disposeGraph = graph.dispose;
  })();

  return () => {
    stopped = true;
    disposeGraph?.();
    disposeGraph = null;
    source.disconnect();
    void audioContext.close();
    stream.getTracks().forEach((track) => track.stop());
  };
}

interface CaptureGraph {
  dispose(): void;
}

async function createCaptureGraph(
  audioContext: AudioContext,
  source: MediaStreamAudioSourceNode,
  push: (channelData: Float32Array) => void,
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

interface Playback {
  play(chunk: ArrayBuffer): void;
  dispose(): void;
}

function createPlayback(): Playback {
  let context: AudioContext | null = null;
  let nextStartTime = 0;

  function ensureContext(): AudioContext {
    if (!context) {
      context = new AudioContext({ sampleRate: SERVER_SAMPLE_RATE });
      nextStartTime = context.currentTime;
    }
    return context;
  }

  return {
    play(chunk) {
      const ctx = ensureContext();
      const samples = new Int16Array(chunk);
      const buffer = ctx.createBuffer(1, samples.length, SERVER_SAMPLE_RATE);
      const channel = buffer.getChannelData(0);
      for (let i = 0; i < samples.length; i++) {
        channel[i] = (samples[i] ?? 0) / 0x7fff;
      }
      const source = ctx.createBufferSource();
      source.buffer = buffer;
      source.connect(ctx.destination);
      const startAt = Math.max(ctx.currentTime, nextStartTime);
      source.start(startAt);
      nextStartTime = startAt + buffer.duration;
    },
    dispose() {
      if (context) {
        void context.close();
        context = null;
      }
    },
  };
}

export type { SessionHooks } from "./session.js";
export { VoiceAgentSession } from "./session.js";
