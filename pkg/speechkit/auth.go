package speechkit

import "context"

// BearerTokenFunc mints a short-lived bearer token for one outbound request.
//
// Providers that talk to customer-owned cloud resources (Microsoft Foundry,
// Azure Speech) accept it next to a static API key: when a host sets it, the
// provider calls it per request and sends "Authorization: Bearer <token>"
// instead of the key. The framework itself never acquires tokens — the host
// decides whether the token comes from a signed-in Entra account, the Azure
// CLI, or anything else — which keeps pkg/speechkit free of identity SDKs
// and credentials alike.
//
// Implementations must be safe for concurrent use and should cache tokens
// until shortly before expiry; providers call them on every request.
type BearerTokenFunc func(ctx context.Context) (string, error)
