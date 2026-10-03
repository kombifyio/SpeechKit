// Default entry — re-exports the browser entry (the most common bundler
// target). Node-specific consumers should import from
// `@kombifyio/speechkit-voiceagent-client/node`.
export * from "./browser.js";
export * from "./protocol.js";
export {
  VoiceAgentSession,
  VoiceAgentClientError,
  deriveWsUrl,
  mintSessionTicket,
  ticketSubprotocol,
  type MintSessionTicketOptions,
  type SessionHooks,
  type SessionOptions,
  type ToolHandler,
  type ToolContext,
  type WireSocket,
} from "./session.js";
