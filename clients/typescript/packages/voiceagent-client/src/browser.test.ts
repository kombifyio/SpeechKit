import { afterEach, describe, expect, it, vi } from "vitest";

import { downsampleToInt16, openBrowserSession } from "./browser.js";
import { CLIENT_SAMPLE_RATE, SERVER_SAMPLE_RATE, type SessionTicket } from "./protocol.js";

const ticket: SessionTicket = {
  session_id: "sess-1",
  ticket: "tkt-1",
  ws_url: "wss://origin.example/v1/voiceagent/sessions/sess-1/ws",
  ws_subprotocol: "ticket.tkt-1",
};

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  binaryType = "blob";
  sent: unknown[] = [];
  readonly listeners = new Map<string, Array<(event: unknown) => void>>();

  constructor(
    readonly url: string,
    readonly protocols?: string | string[],
  ) {
    FakeWebSocket.instances.push(this);
  }

  send(data: unknown): void {
    this.sent.push(data);
  }

  close(): void {}

  addEventListener(type: string, listener: (event: unknown) => void): void {
    const bucket = this.listeners.get(type) ?? [];
    bucket.push(listener);
    this.listeners.set(type, bucket);
  }

  emit(type: string, event?: unknown): void {
    for (const listener of this.listeners.get(type) ?? []) {
      listener(event);
    }
  }
}

interface FakeNode {
  connect: ReturnType<typeof vi.fn>;
  disconnect: ReturnType<typeof vi.fn>;
}

function fakeNode(): FakeNode {
  return { connect: vi.fn(), disconnect: vi.fn() };
}

class FakeScriptProcessor {
  connect = vi.fn();
  disconnect = vi.fn();
  onaudioprocess: ((event: { inputBuffer: { getChannelData(i: number): Float32Array } }) => void) | null = null;
}

class FakeAudioContext {
  static created: FakeAudioContext[] = [];
  static withWorklet = false;
  sampleRate: number;
  currentTime = 0;
  destination = {};
  audioWorklet: { addModule: ReturnType<typeof vi.fn> } | undefined;
  scriptProcessors: FakeScriptProcessor[] = [];
  sources: Array<{ start: ReturnType<typeof vi.fn>; connect: ReturnType<typeof vi.fn>; buffer: unknown }> = [];
  closed = false;

  constructor(options?: { sampleRate?: number }) {
    this.sampleRate = options?.sampleRate ?? 48_000;
    if (FakeAudioContext.withWorklet) {
      this.audioWorklet = { addModule: vi.fn(async () => undefined) };
    }
    FakeAudioContext.created.push(this);
  }

  createMediaStreamSource(): FakeNode {
    return fakeNode();
  }

  createGain(): FakeNode & { gain: { value: number } } {
    return { ...fakeNode(), gain: { value: 1 } };
  }

  createScriptProcessor(): FakeScriptProcessor {
    const processor = new FakeScriptProcessor();
    this.scriptProcessors.push(processor);
    return processor;
  }

  createBuffer(_channels: number, length: number, sampleRate: number) {
    return {
      duration: length / sampleRate,
      channel: new Float32Array(length),
      getChannelData(): Float32Array {
        return this.channel;
      },
    };
  }

  createBufferSource() {
    const source = { start: vi.fn(), connect: vi.fn(), buffer: null as unknown };
    this.sources.push(source);
    return source;
  }

  async close(): Promise<void> {
    this.closed = true;
  }
}

function fakeStream(): MediaStream & { tracks: Array<{ stop: ReturnType<typeof vi.fn> }> } {
  const tracks = [{ stop: vi.fn() }];
  return { tracks, getTracks: () => tracks } as unknown as MediaStream & {
    tracks: Array<{ stop: ReturnType<typeof vi.fn> }>;
  };
}

afterEach(() => {
  FakeWebSocket.instances = [];
  FakeAudioContext.created = [];
  FakeAudioContext.withWorklet = false;
  vi.unstubAllGlobals();
});

function stubBrowserGlobals(): void {
  vi.stubGlobal("WebSocket", FakeWebSocket);
  vi.stubGlobal("AudioContext", FakeAudioContext);
}

describe("openBrowserSession", () => {
  it("opens the server ws_url with the ticket subprotocol", async () => {
    stubBrowserGlobals();
    const handle = await openBrowserSession({
      serverUrl: "https://speechkit.example",
      presetTicket: ticket,
      start: { persona_id: "helper" },
    });
    const socket = FakeWebSocket.instances[0]!;
    expect(socket.url).toBe(ticket.ws_url);
    expect(socket.protocols).toEqual(["ticket.tkt-1"]);
    expect(socket.binaryType).toBe("arraybuffer");
    socket.emit("open");
    expect(JSON.parse(socket.sent[0] as string)).toEqual({ type: "start", persona_id: "helper" });
    expect(handle.ticket).toBe(ticket);
  });

  it("derives ticket.<ticket> when the server omits ws_subprotocol", async () => {
    stubBrowserGlobals();
    const { ws_subprotocol: _omitted, ...bare } = ticket;
    await openBrowserSession({
      serverUrl: "https://speechkit.example",
      presetTicket: bare,
      start: {},
    });
    expect(FakeWebSocket.instances[0]?.protocols).toEqual(["ticket.tkt-1"]);
  });

  it("lets resolveWsUrl rewrite the ws_url to the gateway host", async () => {
    stubBrowserGlobals();
    await openBrowserSession({
      serverUrl: "https://api.kombify.io",
      basePath: "/v1/speechkit",
      presetTicket: ticket,
      resolveWsUrl: (t) => `wss://api.kombify.io/v1/speechkit/voiceagent/sessions/${t.session_id}/ws`,
      start: {},
    });
    const socket = FakeWebSocket.instances[0]!;
    expect(socket.url).toBe("wss://api.kombify.io/v1/speechkit/voiceagent/sessions/sess-1/ws");
    expect(socket.protocols).toEqual(["ticket.tkt-1"]);
  });

  it("mints a ticket through the gateway basePath when none is preset", async () => {
    stubBrowserGlobals();
    const fetchImpl = vi.fn(async (input: RequestInfo | URL) => {
      expect(String(input)).toBe("https://api.kombify.io/v1/speechkit/voiceagent/sessions");
      return new Response(JSON.stringify(ticket), { status: 200 });
    });
    vi.stubGlobal("fetch", fetchImpl);
    const handle = await openBrowserSession({
      serverUrl: "https://api.kombify.io",
      basePath: "/v1/speechkit",
      token: "jwt-1",
      start: {},
    });
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    expect(handle.ticket).toEqual(ticket);
  });
});

describe("attachMicrophone capture pipeline", () => {
  it("falls back to ScriptProcessor and streams resampled PCM16", async () => {
    stubBrowserGlobals();
    const handle = await openBrowserSession({
      serverUrl: "https://speechkit.example",
      presetTicket: ticket,
      start: {},
    });
    const socket = FakeWebSocket.instances[0]!;
    const stream = fakeStream();
    const stop = handle.attachMicrophone(stream);

    const captureContext = FakeAudioContext.created[0]!;
    await vi.waitFor(() => {
      expect(captureContext.scriptProcessors[0]?.onaudioprocess).toBeTypeOf("function");
    });

    const input = new Float32Array(4800).fill(0.5); // 100 ms at 48 kHz
    captureContext.scriptProcessors[0]!.onaudioprocess!({
      inputBuffer: { getChannelData: () => input },
    });

    const chunk = socket.sent.find((entry) => entry instanceof Int16Array) as Int16Array;
    expect(chunk).toBeInstanceOf(Int16Array);
    expect(chunk.length).toBe(4800 / (48_000 / CLIENT_SAMPLE_RATE));
    expect(chunk[0]).toBe(16384); // 0.5 * 0x7fff rounded

    stop();
    expect(stream.tracks[0]?.stop).toHaveBeenCalled();
    expect(captureContext.closed).toBe(true);
  });

  it("prefers the AudioWorklet path when available", async () => {
    stubBrowserGlobals();
    FakeAudioContext.withWorklet = true;
    const workletInstances: Array<{
      port: { onmessage: ((event: { data: Float32Array }) => void) | null };
    }> = [];
    class FakeAudioWorkletNode {
      port: { onmessage: ((event: { data: Float32Array }) => void) | null } = { onmessage: null };
      connect = vi.fn();
      disconnect = vi.fn();
      constructor() {
        workletInstances.push(this);
      }
    }
    vi.stubGlobal("AudioWorkletNode", FakeAudioWorkletNode);

    const handle = await openBrowserSession({
      serverUrl: "https://speechkit.example",
      presetTicket: ticket,
      start: {},
    });
    const socket = FakeWebSocket.instances[0]!;
    handle.attachMicrophone(fakeStream());

    await vi.waitFor(() => {
      expect(workletInstances.at(-1)?.port.onmessage).toBeTypeOf("function");
    });
    const captureContext = FakeAudioContext.created.at(-1)!;
    expect(captureContext.audioWorklet?.addModule).toHaveBeenCalledTimes(1);
    expect(captureContext.scriptProcessors).toHaveLength(0);

    workletInstances.at(-1)!.port.onmessage!({ data: new Float32Array(48).fill(-0.25) });
    const chunk = socket.sent.find((entry) => entry instanceof Int16Array) as Int16Array;
    expect(chunk.length).toBe(16);
    expect(chunk[0]).toBe(-8192); // -0.25 * 0x7fff rounded
  });
});

describe("playChunk playback scheduling", () => {
  it("schedules sequential 24 kHz chunks back to back", async () => {
    stubBrowserGlobals();
    const handle = await openBrowserSession({
      serverUrl: "https://speechkit.example",
      presetTicket: ticket,
      start: {},
    });

    const chunk = new Int16Array(2400).fill(1000); // 100 ms at 24 kHz
    handle.playChunk(chunk.buffer);
    handle.playChunk(chunk.buffer);

    const playbackContext = FakeAudioContext.created.at(-1)!;
    expect(playbackContext.sampleRate).toBe(SERVER_SAMPLE_RATE);
    expect(playbackContext.sources).toHaveLength(2);
    expect(playbackContext.sources[0]?.start).toHaveBeenCalledWith(0);
    expect(playbackContext.sources[1]?.start).toHaveBeenCalledWith(0.1);

    handle.close();
    expect(playbackContext.closed).toBe(true);
  });
});

describe("downsampleToInt16", () => {
  it("passes through at equal rates with int16 conversion", () => {
    const { pcm16, remaining } = downsampleToInt16(new Float32Array([0, 0.5, -0.5, 1]), 16_000, 16_000);
    // Math.round rounds half toward +Infinity: -16383.5 → -16383.
    expect(Array.from(pcm16)).toEqual([0, 16384, -16383, 32767]);
    expect(remaining.length).toBe(0);
  });

  it("downsamples 48 kHz to 16 kHz by picking every third sample", () => {
    const input = new Float32Array(12).map((_, i) => i / 100);
    const { pcm16, remaining } = downsampleToInt16(input, 48_000, 16_000);
    expect(pcm16.length).toBe(4);
    expect(pcm16[0]).toBe(0);
    expect(pcm16[1]).toBe(Math.round(0.03 * 0x7fff));
    expect(pcm16[2]).toBe(Math.round(0.06 * 0x7fff));
    expect(pcm16[3]).toBe(Math.round(0.09 * 0x7fff));
    expect(remaining.length).toBe(0);
  });

  it("returns unconsumed samples for the next call", () => {
    const input = new Float32Array(10).fill(0.1);
    const { pcm16, remaining } = downsampleToInt16(input, 48_000, 16_000);
    expect(pcm16.length).toBe(3); // floor(10 / 3)
    expect(remaining.length).toBe(1); // 10 - floor(3 * 3)
  });

  it("interpolates fractional ticks for non-integer ratios", () => {
    const input = new Float32Array([0, 1, 0, 1, 0, 1, 0, 1]);
    const { pcm16 } = downsampleToInt16(input, 44_100, 16_000);
    expect(pcm16.length).toBe(Math.floor(8 / (44_100 / 16_000)));
    // idx 2.75625 → between input[2]=0 and input[3]=1 at frac ~0.756
    expect(pcm16[1]).toBe(Math.round(0.75625 * 0x7fff));
  });

  it("clamps out-of-range samples to the int16 envelope", () => {
    const { pcm16 } = downsampleToInt16(new Float32Array([2, -2]), 16_000, 16_000);
    expect(pcm16[0]).toBe(0x7fff);
    expect(pcm16[1]).toBe(-0x8000);
  });
});
