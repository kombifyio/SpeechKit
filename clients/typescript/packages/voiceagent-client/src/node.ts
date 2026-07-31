// Node entry. Uses the `ws` peerDependency. Audio I/O is the caller's
// responsibility — this module accepts raw 16 kHz S16 LE mono PCM bytes
// for upload and emits 24 kHz S16 LE mono PCM bytes for playback.
import type { SessionTicket } from "./protocol.js";
import {
  VoiceAgentSession,
  deriveWsUrl,
  mintSessionTicket,
  ticketSubprotocol,
  type SessionOptions,
  type WireSocket,
} from "./session.js";

export interface NodeOpenOptions extends SessionOptions {
  serverUrl: string;
  token?: string;
  /**
   * Path prefix in front of the versioned API route. Defaults to `/v1`
   * (direct server access). Gateway consumers pass e.g.
   * `serverUrl: "https://api.kombify.io"` + `basePath: "/v1/speechkit"`.
   */
  basePath?: string;
  ticket?: Record<string, unknown>;
  presetTicket?: SessionTicket;
  /**
   * Rewrite the WebSocket URL before connecting (e.g. to a gateway
   * host). The upgrade ticket stays in the `Sec-WebSocket-Protocol`
   * subprotocol either way.
   */
  resolveWsUrl?: (ticket: SessionTicket) => string;
  /** Custom fetch (e.g. undici) for ticket minting. */
  fetch?: typeof fetch;
  /**
   * Custom WebSocket constructor. Defaults to a dynamic `import("ws")`
   * which keeps the dependency optional for browser bundlers.
   */
  WebSocketImpl?: WSConstructor;
}

export interface WSConstructor {
  new (url: string, protocols?: string | string[]): WireSocket & { on?: never };
}

export interface NodeSession {
  session: VoiceAgentSession;
  socket: WireSocket;
  ticket: SessionTicket;
  close(): void;
}

/**
 * Node entry point. Mints a ticket (unless `presetTicket` is provided),
 * opens a WebSocket via the supplied or dynamically-imported `ws`
 * package authenticated via the `ticket.<ticket>` subprotocol, and
 * returns the session controller. Audio is bytes-only: call
 * `session.sendAudioChunk(pcm16Buffer)` for outgoing audio and register
 * `hooks.onAudio` for incoming chunks.
 */
export async function openNodeSession(options: NodeOpenOptions): Promise<NodeSession> {
  const ticket =
    options.presetTicket ??
    (await mintSessionTicket({
      serverUrl: options.serverUrl,
      ...(options.token !== undefined ? { token: options.token } : {}),
      ...(options.ticket !== undefined ? { body: options.ticket } : {}),
      ...(options.basePath !== undefined ? { basePath: options.basePath } : {}),
      ...(options.fetch !== undefined ? { fetch: options.fetch } : {}),
    }));
  const url = options.resolveWsUrl
    ? options.resolveWsUrl(ticket)
    : deriveWsUrl(options.serverUrl, ticket, options.basePath);
  const Impl = options.WebSocketImpl ?? (await loadWsImpl());
  const socket = new Impl(url, [ticketSubprotocol(ticket)]);

  const sessionOptions: SessionOptions = {
    start: options.start,
    ...(options.hooks !== undefined ? { hooks: options.hooks } : {}),
    ...(options.tools !== undefined ? { tools: options.tools } : {}),
  };
  const session = new VoiceAgentSession(socket, sessionOptions);
  return {
    session,
    socket,
    ticket,
    close: () => session.close(),
  };
}

async function loadWsImpl(): Promise<WSConstructor> {
  try {
    // Dynamic import keeps `ws` optional at install time (peerDependency).
    const mod = (await import("ws")) as unknown as { WebSocket?: WSConstructor; default?: WSConstructor };
    const ctor = mod.WebSocket ?? mod.default;
    if (!ctor) {
      throw new Error("ws import returned no constructor");
    }
    return ctor;
  } catch (err) {
    throw new Error(
      "@kombifyio/speechkit-voiceagent-client/node requires the `ws` package. Install it with `npm install ws`. Original: " +
        (err as Error).message,
    );
  }
}

export { VoiceAgentSession } from "./session.js";
export type { SessionHooks, SessionOptions, ToolHandler, WireSocket } from "./session.js";
