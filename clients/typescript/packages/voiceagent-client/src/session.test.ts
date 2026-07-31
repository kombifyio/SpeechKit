import { describe, expect, it, vi } from "vitest";

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
  sent: Array<string | ArrayBufferLike | ArrayBufferView> = [];
  closed = false;
  private listeners = new Map<string, Array<(event: unknown) => void>>();

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
  return { socket, session };
}

describe("VoiceAgentSession client framing", () => {
  it("sends the start frame on open", () => {
    const { socket } = openSession();
    expect(socket.sentFrames()[0]).toEqual({ type: "start", persona_id: "helper", locale: "en-US" });
  });

  it("frames text, audio_end, ping, and advance_step", () => {
    const { socket, session } = openSession();
    session.sendText("hello");
    session.endAudio();
    session.ping();
    session.advanceStep("done talking");
    session.advanceStep();
    expect(socket.sentFrames().slice(1)).toEqual([
      { type: "text", text: "hello" },
      { type: "audio_end" },
      { type: "ping" },
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
    expect(onError).toHaveBeenCalledWith(frame);
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

  it("prefers the close event reason and falls back to the code", () => {
    const onClose = vi.fn();
    const { socket } = openSession({ onClose });
    socket.emit("close", { code: 1000, reason: "bye" });
    socket.emit("close", { code: 1006, reason: "" });
    expect(onClose).toHaveBeenNthCalledWith(1, "bye");
    expect(onClose).toHaveBeenNthCalledWith(2, "closed (1006)");
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
        response: { error: "unknown tool: lookup" },
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
        response: { error: "boom" },
      });
    });
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
