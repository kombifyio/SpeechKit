// Default entry — re-exports the browser entry (the most common bundler
// target). Node-specific consumers should import from
// `@kombifyio/speechkit-voiceagent-client/node`.
export * from "./browser.js";
export * from "./protocol.js";
export {
  VoiceAgentSession,
  deriveWsUrl,
  mintSessionTicket,
  ticketSubprotocol,
  type MintSessionTicketOptions,
  type SessionHooks,
  type SessionOptions,
  type ToolHandler,
  type WireSocket,
} from "./session.js";
