import { describe, expect, it, vi } from "vitest";

import { openNodeSession, type WSConstructor } from "./node.js";
import type { SessionTicket } from "./protocol.js";

const ticket: SessionTicket = {
  session_id: "sess-n",
  ticket: "tkt-n",
  ws_url: "wss://origin.example/v1/voiceagent/sessions/sess-n/ws",
  ws_subprotocol: "ticket.tkt-n",
};

class FakeNodeSocket {
  static instances: FakeNodeSocket[] = [];
  sent: unknown[] = [];
  closed = false;
  private listeners = new Map<string, Array<(event: unknown) => void>>();

  constructor(
    readonly url: string,
    readonly protocols?: string | string[],
  ) {
    FakeNodeSocket.instances.push(this);
  }

  send(data: unknown): void {
    this.sent.push(data);
  }

  close(): void {
    this.closed = true;
  }

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

describe("openNodeSession", () => {
  it("connects with the injected WebSocket implementation and ticket subprotocol", async () => {
    FakeNodeSocket.instances = [];
    const handle = await openNodeSession({
      serverUrl: "https://speechkit.example",
      presetTicket: ticket,
      WebSocketImpl: FakeNodeSocket as unknown as WSConstructor,
      start: { locale: "en-US" },
    });
    const socket = FakeNodeSocket.instances[0]!;
    expect(socket.url).toBe(ticket.ws_url);
    expect(socket.protocols).toEqual(["ticket.tkt-n"]);
    socket.emit("open");
    expect(JSON.parse(socket.sent[0] as string)).toEqual({ type: "start", locale: "en-US" });
    expect(handle.ticket).toBe(ticket);

    handle.close();
    expect(socket.closed).toBe(true);
  });

  it("mints through the gateway basePath and applies resolveWsUrl", async () => {
    FakeNodeSocket.instances = [];
    const fetchImpl = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      expect(String(input)).toBe("https://api.kombify.io/v1/speechkit/voiceagent/sessions");
      const headers = (init?.headers ?? {}) as Record<string, string>;
      expect(headers["Authorization"]).toBe("Bearer jwt-n");
      return new Response(JSON.stringify(ticket), { status: 200 });
    });
    await openNodeSession({
      serverUrl: "https://api.kombify.io",
      basePath: "/v1/speechkit",
      token: "jwt-n",
      fetch: fetchImpl as unknown as typeof fetch,
      resolveWsUrl: (t) => `wss://api.kombify.io/v1/speechkit/voiceagent/sessions/${t.session_id}/ws`,
      WebSocketImpl: FakeNodeSocket as unknown as WSConstructor,
      start: {},
    });
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    const socket = FakeNodeSocket.instances[0]!;
    expect(socket.url).toBe("wss://api.kombify.io/v1/speechkit/voiceagent/sessions/sess-n/ws");
    expect(socket.protocols).toEqual(["ticket.tkt-n"]);
  });
});
