package wssession

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
)

// EnvAllowEmptyOrigin opts requests in that do not send an Origin header
// (CLIs, native clients, server-to-server tests) past the WebSocket upgrade
// gate. Default behaviour rejects empty Origin so a browser without CSRF
// protection cannot establish a session by stripping the header.
const EnvAllowEmptyOrigin = "SPEECHKIT_ALLOW_EMPTY_ORIGIN"

// EnvAllowWildcardOrigin permits a literal "*" entry in the allowed-origins
// list to match every Origin. This disables CSRF-style protection for the
// WebSocket upgrade and is intended for local development only. By default a
// "*" entry is dropped (fail-closed) and a loud warning is emitted at handler
// construction, so a stray wildcard in config cannot silently open the
// cross-origin surface in production.
const EnvAllowWildcardOrigin = "SPEECHKIT_ALLOW_WILDCARD_ORIGIN"

// TicketSubprotocolPrefix carries the session ticket as a WebSocket
// subprotocol, e.g. "ticket.AbCd...". This keeps the ticket out of the
// URL and therefore out of any fronting reverse proxy's access log.
const TicketSubprotocolPrefix = "ticket."

// TicketSubprotocol renders the Sec-WebSocket-Protocol value for a ticket.
// Empty tickets yield an empty string.
func TicketSubprotocol(ticket string) string {
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return ""
	}
	return TicketSubprotocolPrefix + ticket
}

// ExtractTicket reads the ticket from the Sec-WebSocket-Protocol subprotocol
// form. The subprotocol must look like "ticket.<value>"; the returned
// subproto string MUST be echoed back to the client via
// AcceptOptions.Subprotocols so the WS handshake completes per RFC 6455.
func ExtractTicket(r *http.Request) (ticket, subproto string) {
	for _, header := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, raw := range strings.Split(header, ",") {
			candidate := strings.TrimSpace(raw)
			if !strings.HasPrefix(candidate, TicketSubprotocolPrefix) {
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(candidate, TicketSubprotocolPrefix))
			if value != "" {
				return value, candidate
			}
		}
	}
	return "", ""
}

// NormalizeAllowedOrigins trims the configured origin allowlist and drops a
// literal "*" unless EnvAllowWildcardOrigin opts the wildcard in (development
// only). Fail-closed by design.
func NormalizeAllowedOrigins(origins []string) []string {
	wildcardOptIn := envBoolTrue(EnvAllowWildcardOrigin)
	out := make([]string, 0, len(origins))
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		if trimmed == "*" && !wildcardOptIn {
			// Fail-closed: drop the wildcard so it cannot match every
			// Origin in production. Warn loudly so a misconfiguration is
			// visible in logs rather than silently honoured.
			slog.Warn("ws session: dropping wildcard '*' from allowed WebSocket origins; "+
				"cross-origin upgrades stay denied. Set "+EnvAllowWildcardOrigin+"=1 to permit it (development only).",
				"hint", "configure explicit origins instead of '*'")
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// UpgradeOriginAllowed reports whether a WebSocket upgrade request may
// proceed to ticket verification. Browser requests always carry an Origin
// header and stay subject to the allowlist (CSRF protection). A request with
// no Origin that presents a ticket subprotocol is a native client — browsers
// cannot omit Origin — and its single-session HMAC ticket (minted via the
// authenticated session POST) is the credential: the upgrade proceeds and
// stands or falls with VerifyTicket immediately after. Ticketless empty-Origin
// requests still require the EnvAllowEmptyOrigin opt-in.
func UpgradeOriginAllowed(r *http.Request, allowed []string) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		if ticket, _ := ExtractTicket(r); ticket != "" {
			return true
		}
		return envBoolTrue(EnvAllowEmptyOrigin)
	}
	return OriginAllowed(origin, allowed)
}

// OriginAllowed reports whether the request Origin may upgrade a WebSocket.
// An empty Origin is denied unless EnvAllowEmptyOrigin opts native/CLI
// clients in.
func OriginAllowed(origin string, allowed []string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		// Default deny: a browser that omits Origin defeats CSRF-style
		// protection. CLIs, native clients, and server-to-server tests
		// that never send Origin can opt in via
		// SPEECHKIT_ALLOW_EMPTY_ORIGIN=1.
		return envBoolTrue(EnvAllowEmptyOrigin)
	}
	for _, candidate := range allowed {
		if candidate == "*" || origin == candidate {
			return true
		}
	}
	return false
}

// envBoolTrue returns true when the named env var holds 1/true/yes/on
// (case-insensitive).
func envBoolTrue(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
