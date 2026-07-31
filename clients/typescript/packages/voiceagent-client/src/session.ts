import {
  TICKET_SUBPROTOCOL_PREFIX,
  type AgentState,
  type ClientFrame,
  type ErrorFrame,
  type EventFrame,
  type ServerFrame,
  type SessionTicket,
  type StartFrame,
  type ToolCallFrame,
} from "./protocol.js";

/**
 * The minimal WebSocket-like surface the session needs. Both the browser
 * `WebSocket` and the Node `ws.WebSocket` satisfy it. Implementations
 * receive `string | ArrayBuffer | Buffer | Blob` from the wire.
 */
export interface WireSocket {
  send(data: string | ArrayBufferLike | ArrayBufferView): void;
  close(code?: number, reason?: string): void;
  addEventListener(type: "open", listener: () => void): void;
  addEventListener(type: "close", listener: (event: { code: number; reason: string }) => void): void;
  addEventListener(type: "error", listener: (event: unknown) => void): void;
  /**
   * `data` is `string` for control frames, `ArrayBuffer` (browser) or
   * `Buffer` (Node `ws` with `binary: true`) for audio chunks.
   */
  addEventListener(type: "message", listener: (event: { data: unknown }) => void): void;
}

export type ToolHandler = (call: ToolCallFrame) => Promise<Record<string, unknown>> | Record<string, unknown>;

export interface SessionHooks {
  onState?(state: AgentState): void;
  onUserTranscript?(text: string, done: boolean): void;
  onAgentTranscript?(text: string, done: boolean): void;
  onAudio?(chunk: ArrayBuffer): void;
  onToolCall?(call: ToolCallFrame): void;
  /** Provider event frames without a more specific v1 mapping. */
  onEvent?(frame: EventFrame): void;
  onError?(err: Error | ErrorFrame): void;
  onClose?(reason: string): void;
}

export interface SessionOptions {
  /** Initial frame sent on `open`. */
  start: Omit<StartFrame, "type">;
  hooks?: SessionHooks;
  /**
   * Map of host-side tools the agent may invoke. The session calls the
   * handler when a `tool_call` frame arrives and forwards the returned
   * value as the `tool_response` payload.
   */
  tools?: Record<string, ToolHandler>;
}

/**
 * Transport-agnostic session controller. Pass a connected
 * {@link WireSocket} (browser or Node) and hooks; the session owns the
 * frame protocol on top.
 */
export class VoiceAgentSession {
  private readonly socket: WireSocket;
  private readonly hooks: SessionHooks;
  private readonly tools: Record<string, ToolHandler>;
  private readonly start: Omit<StartFrame, "type">;
  private opened = false;

  constructor(socket: WireSocket, options: SessionOptions) {
    this.socket = socket;
    this.hooks = options.hooks ?? {};
    this.tools = options.tools ?? {};
    this.start = options.start;

    socket.addEventListener("open", () => {
      this.opened = true;
      this.sendFrame({ type: "start", ...this.start });
    });
    socket.addEventListener("message", (event) => void this.handleMessage(event.data));
    socket.addEventListener("error", (event) => {
      const err = event instanceof Error ? event : new Error("ws error");
      this.hooks.onError?.(err);
    });
    socket.addEventListener("close", (event) => {
      this.opened = false;
      this.hooks.onClose?.(event.reason || `closed (${event.code})`);
    });
  }

  sendText(text: string): void {
    this.sendFrame({ type: "text", text });
  }

  sendAudioChunk(pcm16: ArrayBuffer | ArrayBufferView): void {
    this.socket.send(pcm16);
  }

  endAudio(): void {
    this.sendFrame({ type: "audio_end" });
  }

  ping(): void {
    this.sendFrame({ type: "ping" });
  }

  advanceStep(reason?: string): void {
    this.sendFrame(reason !== undefined ? { type: "advance_step", reason } : { type: "advance_step" });
  }

  close(): void {
    if (this.opened) {
      try {
        this.sendFrame({ type: "stop" });
      } catch {
        /* socket already closed */
      }
    }
    this.socket.close();
  }

  private sendFrame(frame: ClientFrame): void {
    this.socket.send(JSON.stringify(frame));
  }

  private async handleMessage(data: unknown): Promise<void> {
    if (typeof data === "string") {
      let frame: ServerFrame;
      try {
        frame = JSON.parse(data) as ServerFrame;
      } catch (err) {
        this.hooks.onError?.(err as Error);
        return;
      }
      await this.handleFrame(frame);
      return;
    }
    if (data instanceof ArrayBuffer) {
      this.hooks.onAudio?.(data);
      return;
    }
    // Node `ws` delivers Buffer; convert to ArrayBuffer.
    if (typeof data === "object" && data !== null && "buffer" in data && ArrayBuffer.isView(data)) {
      const view = data as ArrayBufferView;
      const ab = view.buffer.slice(view.byteOffset, view.byteOffset + view.byteLength);
      this.hooks.onAudio?.(ab as ArrayBuffer);
    }
  }

  private async handleFrame(frame: ServerFrame): Promise<void> {
    switch (frame.type) {
      case "state":
        this.hooks.onState?.(frame.state);
        return;
      case "input_transcript":
        this.hooks.onUserTranscript?.(frame.text, frame.done);
        return;
      case "output_transcript":
        this.hooks.onAgentTranscript?.(frame.text, frame.done);
        return;
      case "tool_call":
        this.hooks.onToolCall?.(frame);
        await this.dispatchTool(frame);
        return;
      case "event":
        this.hooks.onEvent?.(frame);
        return;
      case "error":
        this.hooks.onError?.(frame);
        return;
      case "session_end":
        this.hooks.onClose?.(frame.reason);
        return;
      case "interrupted":
      case "sequence_step":
      case "pong":
        return;
    }
  }

  private async dispatchTool(call: ToolCallFrame): Promise<void> {
    const handler = this.tools[call.name];
    if (!handler) {
      this.sendFrame({
        type: "tool_response",
        id: call.id,
        name: call.name,
        response: { error: `unknown tool: ${call.name}` },
      });
      return;
    }
    let response: Record<string, unknown>;
    try {
      response = (await handler(call)) ?? {};
    } catch (err) {
      response = { error: (err as Error).message };
    }
    this.sendFrame({
      type: "tool_response",
      id: call.id,
      name: call.name,
      response,
    });
  }
}

export interface MintSessionTicketOptions {
  serverUrl: string;
  token?: string;
  body?: Record<string, unknown>;
  fetch?: typeof fetch;
  /**
   * Path prefix in front of the versioned API route. Defaults to `/v1`
   * (direct server access). Gateway consumers pass e.g.
   * `serverUrl: "https://api.kombify.io"` + `basePath: "/v1/speechkit"`.
   */
  basePath?: string;
}

/**
 * Convenience: POSTs to `{basePath}/voiceagent/sessions` to mint a
 * ticket. Use the result with the {@link openBrowserSession} or
 * {@link openNodeSession} factories.
 */
export async function mintSessionTicket(options: MintSessionTicketOptions): Promise<SessionTicket> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (options.token) headers["Authorization"] = `Bearer ${options.token}`;
  const fetchImpl = options.fetch ?? fetch;
  const base = options.serverUrl.replace(/\/+$/, "");
  const response = await fetchImpl(`${base}${normalizeBasePath(options.basePath)}/voiceagent/sessions`, {
    method: "POST",
    headers,
    body: JSON.stringify(options.body ?? {}),
  });
  if (!response.ok) {
    throw new Error(`speechkit: mint session failed: HTTP ${response.status} ${await response.text()}`);
  }
  return (await response.json()) as SessionTicket;
}

/**
 * Resolve the WebSocket URL for a minted session. Prefers the
 * server-returned `ws_url`; otherwise derives it from `serverUrl` and
 * `basePath`. The ticket is never placed in the URL — pass
 * {@link ticketSubprotocol} as the WebSocket subprotocol instead.
 */
export function deriveWsUrl(serverUrl: string, ticket: SessionTicket, basePath?: string): string {
  if (ticket.ws_url) return ticket.ws_url;
  const wsBase = serverUrl.replace(/^http/, "ws").replace(/\/+$/, "");
  return `${wsBase}${normalizeBasePath(basePath)}/voiceagent/sessions/${encodeURIComponent(ticket.session_id)}/ws`;
}

/**
 * The `Sec-WebSocket-Protocol` value authenticating the upgrade:
 * the server-provided `ws_subprotocol` when present, else
 * `ticket.<ticket>`.
 */
export function ticketSubprotocol(ticket: SessionTicket): string {
  return ticket.ws_subprotocol ?? `${TICKET_SUBPROTOCOL_PREFIX}${ticket.ticket}`;
}

function normalizeBasePath(basePath: string | undefined): string {
  const raw = (basePath ?? "/v1").trim();
  if (raw === "" || raw === "/") return "";
  const withLeading = raw.startsWith("/") ? raw : `/${raw}`;
  return withLeading.replace(/\/+$/, "");
}
