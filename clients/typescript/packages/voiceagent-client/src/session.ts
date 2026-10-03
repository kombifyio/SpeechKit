import {
  TICKET_SUBPROTOCOL_PREFIX,
  type AgentState,
  type ClientFrame,
  type ErrorFrame,
  type EventFrame,
  type InterruptedFrame,
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
  /** Browser WebSocket and Node ws both expose queued outbound bytes. */
  readonly bufferedAmount?: number;
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
  /** Optional for custom shims; native browser/Node sockets remove owned listeners. */
  removeEventListener?(type: string, listener: EventListener): void;
}

export interface ToolContext { signal: AbortSignal }
export type ToolHandler = (call: ToolCallFrame, context?: ToolContext) => Promise<Record<string, unknown>> | Record<string, unknown>;

export interface SessionHooks {
  onState?(state: AgentState): void;
  onUserTranscript?(text: string, done: boolean): void;
  onAgentTranscript?(text: string, done: boolean): void;
  onAudio?(chunk: ArrayBuffer): void;
  onToolCall?(call: ToolCallFrame): void;
  /**
   * The provider cut agent playback short (barge-in). Hosts should drop
   * queued agent audio (e.g. `BrowserSession.flushPlayback()`) and treat
   * the open agent turn as interrupted.
   */
  onInterrupted?(frame: InterruptedFrame): void;
  /** Provider event frames without a more specific v1 mapping. */
  onEvent?(frame: EventFrame): void;
  onError?(err: Error | ErrorFrame): void;
  onClose?(reason: string): void;
}

export interface SessionOptions {
  /** Cancels setup, active transport, and pending host tools. */
  signal?: AbortSignal;
  /** Socket upgrade deadline in milliseconds; defaults to 10 seconds. */
  connectTimeoutMs?: number;
  /** Provider readiness deadline after upgrade; defaults to 20 seconds. */
  readyTimeoutMs?: number;
  /** Maximum queued outbound bytes, including the next frame; defaults to 64,000, capped at 960,000. */
  maxOutboundBytes?: number;
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
  private terminal = false;
  private listening = false;
  private muted = false;
  private toolEpoch = 0;
  private pendingTools = 0;
  private toolAbort = new AbortController();
  private readonly signal: AbortSignal | undefined;
  private readonly readyTimeoutMs: number;
  private readonly maxOutboundBytes: number;
  private readonly abort = () => {
    if (this.terminal) return;
    this.sendStop();
    this.finish("client", new VoiceAgentClientError("session_aborted"));
  };
  private setupTimer: ReturnType<typeof setTimeout>;
  private resolveReady!: () => void;
  private rejectReady!: (error: Error) => void;
  /** Resolves only after listening + session_ready; rejects on setup failure. */
  readonly ready: Promise<void>;
  get isReady(): boolean { return this.listening && !this.terminal; }
  get isClosed(): boolean { return this.terminal; }
  get acceptsAudio(): boolean { return this.isReady && !this.muted; }
  private readonly onOpen = () => {
    if (this.terminal || this.opened) return;
    clearTimeout(this.setupTimer);
    this.setupTimer = setTimeout(() => this.fail("ws_setup_timeout"), this.readyTimeoutMs);
    (this.setupTimer as unknown as { unref?: () => void }).unref?.();
    this.opened = true;
    this.sendFrame({ type: "start", ...this.start });
  };
  private readonly onMessage = (event: { data: unknown }) => {
    void this.handleMessage(event.data).catch(() => this.fail("host_callback_failed"));
  };
  private readonly onSocketError = () => this.fail("ws_failure");
  private readonly onSocketClose = (event?: { code: number; reason: string }) => {
    if (typeof event?.code === "number" && event.code !== 1000 && event.code !== 1001) this.fail("ws_closed");
    else this.finish("closed");
  };

  constructor(socket: WireSocket, options: SessionOptions) {
    this.socket = socket;
    this.hooks = options.hooks ?? {};
    this.tools = options.tools ?? {};
    this.start = options.start;
    this.signal = options.signal;
    this.readyTimeoutMs = positiveLimit(options.readyTimeoutMs, 20_000);
    this.maxOutboundBytes = positiveLimit(options.maxOutboundBytes, MAX_OUTBOUND_BYTES, 960_000);
    this.ready = new Promise<void>((resolve, reject) => {
      this.resolveReady = resolve;
      this.rejectReady = reject;
    });
    // Direct constructor users need not await ready to use hooks safely.
    void this.ready.catch(() => undefined);
    this.setupTimer = setTimeout(() => this.fail("ws_upgrade_timeout"), positiveLimit(options.connectTimeoutMs, 10_000));
    (this.setupTimer as unknown as { unref?: () => void }).unref?.();

    socket.addEventListener("open", this.onOpen);
    socket.addEventListener("message", this.onMessage);
    socket.addEventListener("error", this.onSocketError);
    socket.addEventListener("close", this.onSocketClose);
    this.signal?.addEventListener("abort", this.abort, { once: true });
    if (this.signal?.aborted) this.abort();
  }

  sendText(text: string): void {
    this.sendFrame({ type: "text", text });
  }

  sendAudioChunk(pcm16: ArrayBuffer | ArrayBufferView): void {
    // Never retain or upload microphone bytes before provider readiness.
    if (!this.isReady) return;
    if (pcm16.byteLength === 0 || pcm16.byteLength % 2 !== 0) {
      this.fail("invalid_audio");
      return;
    }
    this.send(pcm16, pcm16.byteLength);
  }

  endAudio(): void {
    this.sendFrame({ type: "audio_end" });
  }

  ping(): void {
    this.sendFrame({ type: "ping" });
  }

  /**
   * Tap-to-interrupt: stops the agent reply that is playing right now.
   * Idempotent and safe while idle. The server answers with an
   * `interrupted` frame either way. Playback is flushed immediately and
   * in-flight downlink audio is muted until that acknowledgement.
   */
  cancel(): void {
    if (this.terminal) return;
    this.muted = true;
    this.invalidateTools();
    try { this.hooks.onInterrupted?.({ type: "interrupted" }); }
    finally { this.sendFrame({ type: "cancel" }); }
  }

  advanceStep(reason?: string): void {
    this.sendFrame(reason !== undefined ? { type: "advance_step", reason } : { type: "advance_step" });
  }

  close(): void {
    if (this.terminal) return;
    this.sendStop();
    this.finish("client");
  }

  private sendStop(): void {
    if (!this.opened || this.terminal) return;
    const stop = JSON.stringify({ type: "stop" });
    if (stop.length + (this.socket.bufferedAmount ?? 0) > this.maxOutboundBytes) return;
    try { this.socket.send(stop); } catch { /* terminal cleanup never retries */ }
  }

  /** Fail closed when a host media adapter cannot retain or play audio. */
  fail(code: string): void {
    this.finish("error", new VoiceAgentClientError(safeCode(code)));
  }

  private invalidateTools(): void {
    this.toolEpoch++;
    this.toolAbort.abort();
    this.toolAbort = new AbortController();
  }

  private finish(reason: string, error?: Error | ErrorFrame): void {
    if (this.terminal) return;
    this.terminal = true;
    this.opened = false;
    this.listening = false;
    clearTimeout(this.setupTimer);
    this.signal?.removeEventListener("abort", this.abort);
    for (const [type, listener] of [
      ["open", this.onOpen], ["message", this.onMessage],
      ["error", this.onSocketError], ["close", this.onSocketClose],
    ] as const) {
      try { this.socket.removeEventListener?.(type, listener as unknown as EventListener); } catch { /* custom shim */ }
    }
    this.invalidateTools();
    this.toolAbort.abort();
    this.rejectReady(error instanceof Error ? error : new VoiceAgentClientError(error?.code ?? "ws_setup_failed"));
    try { this.socket.close(); } catch { /* transport is already unavailable */ }
    // A host callback must never prevent the remaining terminal cleanup.
    try { if (error) this.hooks.onError?.(error); } catch { /* host callback */ }
    try { this.hooks.onClose?.(reason); } catch { /* host callback */ }
  }

  private sendFrame(frame: ClientFrame): void {
    if (this.terminal) return;
    let text: string;
    try { text = JSON.stringify(frame); }
    catch { this.fail("invalid_control"); return; }
    const bytes = new TextEncoder().encode(text).byteLength;
    if (bytes > MAX_CONTROL_BYTES) { this.fail("voice_buffer_overflow"); return; }
    this.send(text, bytes);
  }

  private send(data: string | ArrayBufferLike | ArrayBufferView, bytes: number): void {
    if (this.terminal) return;
    if (!this.opened) { this.fail("ws_not_ready"); return; }
    if (bytes + (this.socket.bufferedAmount ?? 0) > this.maxOutboundBytes) {
      this.fail("voice_buffer_overflow");
      return;
    }
    try { this.socket.send(data); }
    catch { this.fail("ws_send_failed"); }
  }

  private async handleMessage(data: unknown): Promise<void> {
    if (this.terminal) return;
    if (typeof data === "string") {
      if (data.length > MAX_CONTROL_BYTES || new TextEncoder().encode(data).byteLength > MAX_CONTROL_BYTES) {
        this.fail("voice_buffer_overflow"); return;
      }
      let frame: ServerFrame;
      try {
        frame = JSON.parse(data) as ServerFrame;
      } catch { this.fail("invalid_control"); return; }
      if (!frame || typeof frame !== "object" || typeof frame.type !== "string") {
        this.fail("invalid_control"); return;
      }
      await this.handleFrame(frame);
      return;
    }
    if (data instanceof ArrayBuffer) {
      this.receiveAudio(data);
      return;
    }
    // Node `ws` delivers Buffer; convert to ArrayBuffer.
    if (typeof data === "object" && data !== null && "buffer" in data && ArrayBuffer.isView(data)) {
      const view = data as ArrayBufferView;
      if (!this.acceptsAudio) return;
      if (!view.byteLength || view.byteLength % 2 || view.byteLength > MAX_PLAYBACK_BYTES) {
        this.fail("voice_buffer_overflow"); return;
      }
      const ab = view.buffer.slice(view.byteOffset, view.byteOffset + view.byteLength);
      this.receiveAudio(ab as ArrayBuffer);
    }
  }

  private receiveAudio(data: ArrayBuffer): void {
    if (!this.acceptsAudio) return;
    if (!data.byteLength || data.byteLength % 2 || data.byteLength > MAX_PLAYBACK_BYTES) {
      this.fail("voice_buffer_overflow"); return;
    }
    this.hooks.onAudio?.(data);
  }

  private async handleFrame(frame: ServerFrame): Promise<void> {
    switch (frame.type) {
      case "state":
        if (this.opened && frame.state === "listening" && frame.event_type === "session_ready" && !this.listening) {
          this.listening = true;
          clearTimeout(this.setupTimer);
          this.resolveReady();
        }
        this.hooks.onState?.(frame.state);
        return;
      case "input_transcript":
        if (typeof frame.text !== "string") { this.fail("invalid_control"); return; }
        this.hooks.onUserTranscript?.(frame.text, frame.done === true);
        return;
      case "output_transcript":
        if (typeof frame.text !== "string") { this.fail("invalid_control"); return; }
        this.hooks.onAgentTranscript?.(frame.text, frame.done === true);
        return;
      case "tool_call":
        this.hooks.onToolCall?.(frame);
        await this.dispatchTool(frame);
        return;
      case "event":
        this.hooks.onEvent?.(frame);
        return;
      case "error":
        // The server owns messages/remediation; only the stable code is used
        // for lifecycle decisions. Optional fatal defaults to false.
        const error = { ...frame, code: safeCode(frame.code), fatal: frame.fatal === true };
        if (error.fatal) this.finish(error.code === "auth_expired" ? "authorization_expired" : "error", error);
        else this.hooks.onError?.(error);
        return;
      case "session_end":
        this.finish(safeReason(frame.reason));
        return;
      case "interrupted":
        this.muted = false;
        this.invalidateTools();
        this.hooks.onInterrupted?.(frame);
        return;
      case "sequence_step":
      case "pong":
        return;
    }
  }

  private async dispatchTool(call: ToolCallFrame): Promise<void> {
    if (!this.isReady) return;
    if (typeof call.id !== "string" || typeof call.name !== "string") { this.fail("invalid_control"); return; }
    if (this.pendingTools >= 8) { this.fail("voice_buffer_overflow"); return; }
    const epoch = this.toolEpoch;
    const signal = this.toolAbort.signal;
    const handler = Object.hasOwn(this.tools, call.name) ? this.tools[call.name] : undefined;
    if (!handler) {
      this.sendFrame({
        type: "tool_response",
        id: call.id,
        name: call.name,
        response: { error: "unknown_tool" },
      });
      return;
    }
    let response: Record<string, unknown>;
    this.pendingTools++;
    try {
      response = (await handler(call, { signal })) ?? {};
    } catch { response = { error: "tool_failed" }; }
    finally { this.pendingTools--; }
    if (this.terminal || signal.aborted || epoch !== this.toolEpoch) return;
    this.sendFrame({
      type: "tool_response",
      id: call.id,
      name: call.name,
      response,
    });
  }
}

export interface MintSessionTicketOptions {
  signal?: AbortSignal;
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
  throwIfAborted(options.signal);
  const mintAbort = new AbortController();
  const abort = () => mintAbort.abort();
  let expired = false;
  const deadline = setTimeout(() => { expired = true; mintAbort.abort(); }, 15_000);
  (deadline as unknown as { unref?: () => void }).unref?.();
  options.signal?.addEventListener("abort", abort, { once: true });
  try {
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (options.token) headers["Authorization"] = `Bearer ${options.token}`;
    const fetchImpl = options.fetch ?? fetch;
    const base = trimTrailingSlashes(options.serverUrl);
    throwIfAborted(options.signal);
    const response = await abortable(fetchImpl(`${base}${normalizeBasePath(options.basePath)}/voiceagent/sessions`, {
      method: "POST",
      headers,
      body: JSON.stringify(options.body ?? {}),
      signal: mintAbort.signal,
    }), mintAbort.signal);
    if (!response.ok) {
      throw new VoiceAgentClientError("ticket_mint_failed", `speechkit: mint session failed: HTTP ${response.status}`);
    }
    const ticket = await abortable(response.json() as Promise<SessionTicket>, mintAbort.signal);
    throwIfAborted(options.signal);
    return ticket;
  } catch (error) {
    throwIfAborted(options.signal);
    if (expired) throw new VoiceAgentClientError("ticket_mint_timeout");
    if (error instanceof VoiceAgentClientError) throw error;
    throw new VoiceAgentClientError("ticket_mint_failed");
  } finally {
    clearTimeout(deadline);
    options.signal?.removeEventListener("abort", abort);
  }
}

export const MAX_CONTROL_BYTES = 65_536;
export const MAX_OUTBOUND_BYTES = 64_000; // Two seconds of 16 kHz S16 microphone audio.
export const MAX_PLAYBACK_BYTES = 96_000; // Two seconds of 24 kHz S16 playback.

export class VoiceAgentClientError extends Error {
  constructor(readonly code: string, message = code) { super(message); this.name = "VoiceAgentClientError"; }
}

/** Invalid or overflowing host budgets retain the bounded default. */
function positiveLimit(value: number | undefined, fallback: number, maximum = 2_147_483_647): number {
  return value !== undefined && Number.isFinite(value) && value >= 1 && value <= maximum
    ? Math.floor(value) : fallback;
}

function safeCode(code: unknown): string {
  return typeof code === "string" && /^[a-z][a-z0-9_]{0,63}$/.test(code) ? code : "turn_failed";
}
function safeReason(reason: unknown): string {
  return ["idle", "go_away", "client", "error", "shutdown", "max_duration", "authorization_expired"].includes(reason as string)
    ? reason as string : "error";
}
export function throwIfAborted(signal?: AbortSignal): void {
  if (signal?.aborted) throw new VoiceAgentClientError("session_aborted");
}
/** Observes cancellation even when a custom fetch/import ignores its signal. */
export function abortable<T>(work: Promise<T>, signal?: AbortSignal): Promise<T> {
  if (!signal) return work;
  return new Promise<T>((resolve, reject) => {
    const abort = () => { cleanup(); reject(new VoiceAgentClientError("session_aborted")); };
    const cleanup = () => signal.removeEventListener("abort", abort);
    work.then(value => { cleanup(); resolve(value); }, error => { cleanup(); reject(error); });
    signal.addEventListener("abort", abort, { once: true });
    if (signal.aborted) abort();
  });
}

/**
 * Resolve the WebSocket URL for a minted session. Prefers the
 * server-returned `ws_url`; otherwise derives it from `serverUrl` and
 * `basePath`. The ticket is never placed in the URL — pass
 * {@link ticketSubprotocol} as the WebSocket subprotocol instead.
 */
export function deriveWsUrl(serverUrl: string, ticket: SessionTicket, basePath?: string): string {
  if (ticket.ws_url) return ticket.ws_url;
  const wsBase = trimTrailingSlashes(serverUrl.replace(/^http/, "ws"));
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

function trimTrailingSlashes(value: string): string {
  let end = value.length;
  while (end > 0 && value.charCodeAt(end - 1) === 47) end--;
  return value.slice(0, end);
}

function normalizeBasePath(basePath: string | undefined): string {
  const raw = (basePath ?? "/v1").trim();
  if (raw === "" || raw === "/") return "";
  const withLeading = raw.startsWith("/") ? raw : `/${raw}`;
  return trimTrailingSlashes(withLeading);
}
