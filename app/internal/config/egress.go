package config

import "github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"

// ApplyEgressPolicy installs the process-wide netsec egress policy for the
// config's effective network scope. Every guarded client and the STT
// connectivity probe are bound by it, so the scope holds at the socket even
// where a call site forgot its own check. A host calls it at start-up and
// whenever the scope or the setup-traffic opt-in changes outside the owner.
func ApplyEgressPolicy(cfg *Config) {
	netsec.Configure(cfg.NetworkScope().EgressPolicy(cfg.SetupTrafficAllowed()))
}

// WatchEgressPolicy re-installs the egress policy on every committed change
// of the network scope or the setup-traffic opt-in, whatever its cause: a
// running policy refresh tightens it at once. It returns the func that stops
// it.
func WatchEgressPolicy(shared *Shared) (stop func()) {
	return shared.Subscribe(func(ch Change) {
		if ch.Old.NetworkScope() != ch.New.NetworkScope() || ch.Old.SetupTrafficAllowed() != ch.New.SetupTrafficAllowed() {
			ApplyEgressPolicy(ch.New)
		}
	})
}
