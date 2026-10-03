import { afterEach, describe, expect, it, vi } from "vitest";

import type { ErrorFrame, EventFrame, SessionTicket, ToolCallFrame } from "./protocol.js";
import {
  VoiceAgentSession,
  deriveWsUrl,
  mintSessionTicket,
  ticketSubprotocol,
  type SessionHooks,
  type WireSocket,
} from "./session.js";

type Listener = (event: never) => void;

class FakeSocket implements WireSocket {
  static instances: FakeSocket[] = [];
  bufferedAmount = 0;
  sent: Array<string | ArrayBufferLike | ArrayBufferView> = [];
  closed = false;
  private listeners = new Map<string, Array<(event: unknown) => void>>();
  constructor() { FakeSocket.instances.push(this); }

  send(data: string | ArrayBufferLike | ArrayBufferView): void {
    this.sent.push(data);
  }

  close(): void {
    this.closed = true;
  }

  addEventListener(type: string, listener: Listener): void {
    const bucket = this.listeners.get(type) ?? [];
    bucket.push(listener as (event: unknown) => void);
    this.listeners.set(type, bucket);
  }

  emit(type: string, event?: unknown): void {
    for (const listener of this.listeners.get(type) ?? []) {
      listener(event);
    }
  }

  sentFrames(): Array<Record<string, unknown>> {
    return this.sent
      .filter((entry): entry is string => typeof entry === "string")
      .map((entry) => JSON.parse(entry) as Record<string, unknown>);
  }
}

function openSession(hooks: SessionHooks = {}, tools = {}) {
  const socket = new FakeSocket();
  const session = new VoiceAgentSession(socket, {
    start: { persona_id: "helper", locale: "en-US" },
    hooks,
    tools,
  });
  socket.emit("open");
  socket.emit("message", { data: JSON.stringify({ type: "state", state: "listening", event_type: "session_ready" }) });
  return { socket, session };
}
afterEach(() => {
  for (const socket of FakeSocket.instances) socket.emit("close", { code: 1000, reason: "" });
  FakeSocket.instances = [];
  vi.useRealTimers();
});

describe("VoiceAgentSession client framing", () => {
  it("sends the start frame on open", () => {
    const { socket } = openSession();
    expect(socket.sentFrames()[0]).toEqual({ type: "start", persona_id: "helper", locale: "en-US" });
  });

  it("frames text, audio_end, ping, cancel, and advance_step", () => {
    const { socket, session } = openSession();
    session.sendText("hello");
    session.endAudio();
    session.ping();
    session.cancel();
    session.advanceStep("done talking");
    session.advanceStep();
    expect(socket.sentFrames().slice(1)).toEqual([
      { type: "text", text: "hello" },
      { type: "audio_end" },
      { type: "ping" },
      { type: "cancel" },
      { type: "advance_step", reason: "done talking" },
      { type: "advance_step" },
    ]);
  });

  it("sends binary audio chunks unframed", () => {
    const { socket, session } = openSession();
    const chunk = new Int16Array([1, -2, 3]);
    session.sendAudioChunk(chunk);
    expect(socket.sent).toContain(chunk);
  });

  it("sends stop and closes the socket on close()", () => {
    const { socket, session } = openSession();
    session.close();
    expect(socket.sentFrames().at(-1)).toEqual({ type: "stop" });
    expect(socket.closed).toBe(true);
  });

  it("skips the stop frame when never opened", () => {
    const socket = new FakeSocket();
    const session = new VoiceAgentSession(socket, { start: {} });
    session.close();
    expect(socket.sentFrames()).toEqual([]);
    expect(socket.closed).toBe(true);
  });
});

describe("VoiceAgentSession server frame dispatch", () => {
  it("routes state and transcript frames to hooks", () => {
    const onState = vi.fn();
    const onUserTranscript = vi.fn();
    const onAgentTranscript = vi.fn();
    const { socket } = openSession({ onState, onUserTranscript, onAgentTranscript });
    socket.emit("message", { data: JSON.stringify({ type: "state", state: "listening" }) });
    socket.emit("message", { data: JSON.stringify({ type: "input_transcript", text: "hi", done: false }) });
    socket.emit("message", { data: JSON.stringify({ type: "output_transcript", text: "hey", done: true }) });
    expect(onState).toHaveBeenCalledWith("listening");
    expect(onUserTranscript).toHaveBeenCalledWith("hi", false);
    expect(onAgentTranscript).toHaveBeenCalledWith("hey", true);
  });

  it("routes event frames to onEvent", () => {
    const onEvent = vi.fn();
    const { socket } = openSession({ onEvent });
    const frame: EventFrame = { type: "event", event_type: "turn_end" };
    socket.emit("message", { data: JSON.stringify(frame) });
    expect(onEvent).toHaveBeenCalledWith(frame);
  });

  it("routes interrupted frames to onInterrupted", () => {
    const onInterrupted = vi.fn();
    const { socket } = openSession({ onInterrupted });
    const frame = { type: "interrupted", event_type: "interrupted" };
    socket.emit("message", { data: JSON.stringify(frame) });
    expect(onInterrupted).toHaveBeenCalledWith(frame);
  });

  it("sends the per-session provider in the start frame", () => {
    const socket = new FakeSocket();
    new VoiceAgentSession(socket, { start: { provider: "deepgram" } });
    socket.emit("open");
    expect(socket.sentFrames()[0]).toEqual({ type: "start", provider: "deepgram" });
  });

  it("passes error frames with remediation and request_id through", () => {
    const onError = vi.fn();
    const { socket } = openSession({ onError });
    const frame: ErrorFrame = {
      type: "error",
      code: "speechkit_feature_not_entitled",
      message: "denied",
      remediation: "upgrade the workspace plan",
      request_id: "req-42",
    };
    socket.emit("message", { data: JSON.stringify(frame) });
    expect(onError).toHaveBeenCalledWith({ ...frame, fatal: false });
  });

  it("maps session_end (incl. max_duration) to onClose", () => {
    const onClose = vi.fn();
    const { socket } = openSession({ onClose });
    socket.emit("message", { data: JSON.stringify({ type: "session_end", reason: "max_duration" }) });
    expect(onClose).toHaveBeenCalledWith("max_duration");
  });

  it("delivers binary ArrayBuffer messages to onAudio", () => {
    const onAudio = vi.fn();
    const { socket } = openSession({ onAudio });
    const chunk = new Int16Array([7, 8]).buffer;
    socket.emit("message", { data: chunk });
    expect(onAudio).toHaveBeenCalledWith(chunk);
  });

  it("converts Node Buffer-style views to a tightly-sliced ArrayBuffer", () => {
    const onAudio = vi.fn();
    const { socket } = openSession({ onAudio });
    const backing = new Uint8Array([0, 0, 1, 2, 3, 4, 0, 0]);
    const view = new Uint8Array(backing.buffer, 2, 4);
    socket.emit("message", { data: view });
    expect(onAudio).toHaveBeenCalledTimes(1);
    const received = onAudio.mock.calls[0]?.[0] as ArrayBuffer;
    expect(Array.from(new Uint8Array(received))).toEqual([1, 2, 3, 4]);
  });

  it("reports malformed JSON control frames to onError", () => {
    const onError = vi.fn();
    const { socket } = openSession({ onError });
    socket.emit("message", { data: "{not json" });
    expect(onError).toHaveBeenCalledTimes(1);
    expect(onError.mock.calls[0]?.[0]).toBeInstanceOf(Error);
  });

  it("closes locally once without exposing arbitrary peer close text", () => {
    const onClose = vi.fn();
    const { socket } = openSession({ onClose });
    socket.emit("close", { code: 1000, reason: "bye" });
    socket.emit("close", { code: 1006, reason: "" });
    expect(onClose.mock.calls).toEqual([["closed"]]);
  });
});

describe("VoiceAgentSession tool dispatch", () => {
  const call: ToolCallFrame = { type: "tool_call", id: "t1", name: "lookup", args: { q: "x" } };

  it("invokes the registered handler and forwards its response", async () => {
    const onToolCall = vi.fn();
    const { socket } = openSession({ onToolCall }, { lookup: () => ({ answer: 42 }) });
    socket.emit("message", { data: JSON.stringify(call) });
    await vi.waitFor(() => {
      expect(socket.sentFrames().at(-1)).toEqual({
        type: "tool_response",
        id: "t1",
        name: "lookup",
        response: { answer: 42 },
      });
    });
    expect(onToolCall).toHaveBeenCalledWith(call);
  });

  it("answers unknown tools with an error response", async () => {
    const { socket } = openSession();
    socket.emit("message", { data: JSON.stringify(call) });
    await vi.waitFor(() => {
      expect(socket.sentFrames().at(-1)).toEqual({
        type: "tool_response",
        id: "t1",
        name: "lookup",
        response: { error: "unknown_tool" },
      });
    });
  });

  it("converts a throwing handler into an error response", async () => {
    const { socket } = openSession(
      {},
      {
        lookup: async () => {
          throw new Error("boom");
        },
      },
    );
    socket.emit("message", { data: JSON.stringify(call) });
    await vi.waitFor(() => {
      expect(socket.sentFrames().at(-1)).toEqual({
        type: "tool_response",
        id: "t1",
        name: "lookup",
        response: { error: "tool_failed" },
      });
    });
  });
});

describe("VoiceAgentSession reliability", () => {
  it("withholds microphone audio until the actual provider ready frame", async () => {
    const socket = new FakeSocket();
    const session = new VoiceAgentSession(socket, { start: {} });
    const pcm = new Int16Array([1, 2]);
    socket.emit("open");
    session.sendAudioChunk(pcm);
    socket.emit("message", { data: JSON.stringify({ type: "state", state: "processing", event_type: "session_ready" }) });
    session.sendAudioChunk(pcm);
    expect(socket.sent.filter(data => typeof data !== "string")).toEqual([]);
    socket.emit("message", { data: JSON.stringify({ type: "state", state: "listening", event_type: "session_ready" }) });
    await session.ready;
    session.sendAudioChunk(pcm);
    expect(socket.sent.at(-1)).toBe(pcm);
  });

  it("bounds silent upgrade and provider setup independently without peer cooperation", async () => {
    vi.useFakeTimers();
    for (const opened of [false, true]) {
      const socket = new FakeSocket();
      const onClose = vi.fn();
      const session = new VoiceAgentSession(socket, { start: {}, hooks: { onClose } });
      const ready = expect(session.ready).rejects.toMatchObject({ code: opened ? "ws_setup_timeout" : "ws_upgrade_timeout" });
      if (opened) {
        await vi.advanceTimersByTimeAsync(9_000);
        socket.emit("open");
      }
      await vi.advanceTimersByTimeAsync(opened ? 20_000 : 10_000);
      await ready;
      expect(socket.closed).toBe(true);
      expect(onClose.mock.calls).toEqual([["error"]]);
    }
  });

  it("aborts pending setup and ignores late socket readiness and audio", async () => {
    const abort = new AbortController();
    const socket = new FakeSocket();
    const onAudio = vi.fn();
    const session = new VoiceAgentSession(socket, { start: {}, signal: abort.signal, hooks: { onAudio } });
    const ready = expect(session.ready).rejects.toMatchObject({ code: "session_aborted" });
    abort.abort();
    await ready;
    socket.emit("open");
    socket.emit("message", { data: JSON.stringify({ type: "state", state: "listening", event_type: "session_ready" }) });
    socket.emit("message", { data: new Int16Array([1, 2]).buffer });
    expect(socket.closed).toBe(true);
    expect(socket.sent).toEqual([]);
    expect(onAudio).not.toHaveBeenCalled();
  });

  it("keeps recoverable errors usable and ends fatal authorization once", () => {
    const onError = vi.fn();
    const onClose = vi.fn();
    const onAudio = vi.fn();
    const { session, socket } = openSession({ onError, onClose, onAudio });
    socket.emit("message", { data: JSON.stringify({ type: "error", code: "tts_failed", message: "synthesis failed" }) });
    expect(session.isReady).toBe(true);
    expect(onError.mock.calls[0]?.[0]).toMatchObject({ code: "tts_failed", fatal: false });
    socket.emit("message", { data: JSON.stringify({ type: "error", code: "auth_expired", message: "expired", fatal: true }) });
    socket.emit("message", { data: JSON.stringify({ type: "session_end", reason: "authorization_expired" }) });
    socket.emit("close", { code: 1000, reason: "private provider text" });
    socket.emit("message", { data: new Int16Array([1, 2]).buffer });
    expect(socket.closed).toBe(true);
    expect(onClose.mock.calls).toEqual([["authorization_expired"]]);
    expect(onAudio).not.toHaveBeenCalled();
  });

  it("fails closed before adding audio to a saturated outbound socket", () => {
    const onError = vi.fn();
    const { session, socket } = openSession({ onError });
    socket.bufferedAmount = 63_998;
    const pcm = new Int16Array([1, 2]);
    session.sendAudioChunk(pcm);
    expect(socket.sent).not.toContain(pcm);
    expect(socket.closed).toBe(true);
    expect(onError.mock.calls[0]?.[0]).toMatchObject({ code: "voice_buffer_overflow" });
  });

  it("preserves complete valid transcript deltas and rejects oversized wire controls", () => {
    const onAgentTranscript = vi.fn();
    const onError = vi.fn();
    const { socket } = openSession({ onAgentTranscript, onError });
    const text = "[turn_failed] " + "provider detail ".repeat(2_000) + "latest reply";
    socket.emit("message", { data: JSON.stringify({ type: "output_transcript", text, done: true }) });
    expect(onAgentTranscript.mock.calls[0]?.[0]).toBe(text);
    socket.emit("message", { data: " ".repeat(65_537) });
    expect(socket.closed).toBe(true);
    expect(onError.mock.calls[0]?.[0]).toMatchObject({ code: "voice_buffer_overflow" });
  });

  it("cancels host tools and never sends their late result", async () => {
    let resolve!: (response: Record<string, unknown>) => void;
    let toolSignal!: AbortSignal;
    const lookup = vi.fn((_call: ToolCallFrame, context?: { signal: AbortSignal }) => {
      toolSignal = context!.signal;
      return new Promise<Record<string, unknown>>(done => { resolve = done; });
    });
    const { session, socket } = openSession({}, { lookup });
    socket.emit("message", { data: JSON.stringify({ type: "tool_call", id: "t1", name: "lookup" }) });
    session.cancel();
    expect(toolSignal.aborted).toBe(true);
    resolve({ answer: 42 });
    await Promise.resolve();
    await Promise.resolve();
    expect(socket.sentFrames().filter(frame => frame.type === "tool_response")).toEqual([]);
  });
});

describe("mintSessionTicket", () => {
  const ticket: SessionTicket = {
    session_id: "sess-9",
    ticket: "tkt-9",
    ws_url: "wss://origin.example/v1/voiceagent/sessions/sess-9/ws",
    ws_subprotocol: "ticket.tkt-9",
  };

  function mintFetch() {
    const requests: Array<{ url: string; init: RequestInit }> = [];
    const impl = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      requests.push({ url: String(input), init: init ?? {} });
      return new Response(JSON.stringify(ticket), { status: 200 });
    });
    return { impl: impl as unknown as typeof fetch, requests };
  }

  it("posts to the default /v1 voiceagent sessions route", async () => {
    const { impl, requests } = mintFetch();
    const minted = await mintSessionTicket({ serverUrl: "http://localhost:8080/", fetch: impl });
    expect(minted).toEqual(ticket);
    expect(requests[0]?.url).toBe("http://localhost:8080/v1/voiceagent/sessions");
    expect(requests[0]?.init.method).toBe("POST");
  });

  it("respects a gateway basePath and bearer token", async () => {
    const { impl, requests } = mintFetch();
    await mintSessionTicket({
      serverUrl: "https://api.kombify.io",
      basePath: "/v1/speechkit",
      token: "jwt-1",
      body: { persona_id: "helper" },
      fetch: impl,
    });
    expect(requests[0]?.url).toBe("https://api.kombify.io/v1/speechkit/voiceagent/sessions");
    const headers = requests[0]?.init.headers as Record<string, string>;
    expect(headers["Authorization"]).toBe("Bearer jwt-1");
    expect(requests[0]?.init.body).toBe(JSON.stringify({ persona_id: "helper" }));
  });

  it("throws on a non-2xx mint response", async () => {
    const impl = (async () => new Response("denied", { status: 403 })) as unknown as typeof fetch;
    await expect(mintSessionTicket({ serverUrl: "http://localhost:8080", fetch: impl })).rejects.toThrow(
      /HTTP 403/,
    );
  });

  it("cancels a pending mint even if a custom fetch ignores AbortSignal", async () => {
    const abort = new AbortController();
    const impl = vi.fn(() => new Promise<Response>(() => undefined)) as unknown as typeof fetch;
    const mint = mintSessionTicket({ serverUrl: "https://speechkit.example", fetch: impl, signal: abort.signal });
    abort.abort();
    await expect(mint).rejects.toMatchObject({ code: "session_aborted" });
  });

  it("expires stalled ticket minting and aborts its request without replay", async () => {
    vi.useFakeTimers();
    let requestSignal!: AbortSignal;
    const impl = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      requestSignal = init!.signal!;
      return new Promise<Response>(() => undefined);
    }) as unknown as typeof fetch;
    const mint = mintSessionTicket({ serverUrl: "https://speechkit.example", fetch: impl });
    const rejected = expect(mint).rejects.toMatchObject({ code: "ticket_mint_timeout" });
    await vi.advanceTimersByTimeAsync(15_000);
    await rejected;
    expect(requestSignal.aborted).toBe(true);
    expect(impl).toHaveBeenCalledTimes(1);
  });
});

describe("deriveWsUrl and ticketSubprotocol", () => {
  it("prefers the server-returned ws_url", () => {
    const url = deriveWsUrl("http://localhost:8080", {
      session_id: "s1",
      ticket: "t1",
      ws_url: "wss://public.example/v1/voiceagent/sessions/s1/ws",
    });
    expect(url).toBe("wss://public.example/v1/voiceagent/sessions/s1/ws");
  });

  it("derives a ws URL without leaking the ticket into it", () => {
    const url = deriveWsUrl("https://speechkit.example/", { session_id: "s 1", ticket: "secret" });
    expect(url).toBe("wss://speechkit.example/v1/voiceagent/sessions/s%201/ws");
    expect(url).not.toContain("secret");
  });

  it("derives through a gateway basePath", () => {
    const url = deriveWsUrl(
      "https://api.kombify.io",
      { session_id: "s1", ticket: "t1" },
      "/v1/speechkit",
    );
    expect(url).toBe("wss://api.kombify.io/v1/speechkit/voiceagent/sessions/s1/ws");
  });

  it("uses ws_subprotocol when present and derives ticket.<ticket> otherwise", () => {
    expect(ticketSubprotocol({ session_id: "s1", ticket: "t1", ws_subprotocol: "ticket.override" })).toBe(
      "ticket.override",
    );
    expect(ticketSubprotocol({ session_id: "s1", ticket: "t1" })).toBe("ticket.t1");
  });
});
