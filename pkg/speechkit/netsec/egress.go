package netsec

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// Policy is the process-wide egress rule every client from [NewClient] and
// every [DialContext] dial is bound by. The zero value is unrestricted (the
// open network scope): call sites keep their own endpoint validation.
type Policy struct {
	// Validation, when non-nil, must pass for every dialed IP and every
	// redirect target of every guarded client. A host derives it from its
	// resolved network scope (see speechkit.NetworkScope.EgressPolicy).
	Validation *ValidationOptions

	// AllowSetupTraffic lets clients marked [ClientOptions.SetupTraffic] (model
	// downloads, update checks) skip Validation. A host sets it from the
	// scope's explicit setup-traffic opt-in.
	AllowSetupTraffic bool
}

// AllowsPublicEgress reports whether a dial to a public address can succeed
// under this policy. A host uses it to skip work that only makes sense with
// internet access, such as a connectivity probe.
func (p Policy) AllowsPublicEgress() bool {
	return p.Validation == nil || !p.Validation.RequireLocal
}

var activePolicy atomic.Pointer[Policy]

// Configure installs the process-wide egress policy. The host calls it at
// start and again whenever the network scope changes; guarded clients built
// earlier pick the new policy up on their next dial. Configure(Policy{})
// restores the unrestricted default.
func Configure(p Policy) {
	if p.Validation != nil {
		v := *p.Validation
		p.Validation = &v
	}
	activePolicy.Store(&p)
}

// CurrentPolicy returns the policy installed by [Configure].
func CurrentPolicy() Policy { return currentPolicy() }

func currentPolicy() Policy {
	if p := activePolicy.Load(); p != nil {
		return *p
	}
	return Policy{}
}

var defaultClient = NewClient(ClientOptions{Timeout: 30 * time.Second})

// Default returns the shared guarded client (30 second timeout). Callers must
// not modify it; use [NewClient] for a different timeout.
func Default() *http.Client { return defaultClient }

// DialContext opens a raw connection through the same guard as the HTTP
// clients: the address is validated under the active [Policy] before the
// socket opens, and a refused address returns a netsec error without dialing.
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return guardedDialContext(dialer, nil, false)(ctx, network, address)
}
