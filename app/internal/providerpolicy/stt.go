// Package providerpolicy applies the provider allowlist and the retention
// scope (config.Config.ProviderConstructionAllowed) to the STT provider sets
// the Device-Target and the Server-Target assemble, so a refused provider is
// never constructed whichever host builds the router. It links no provider
// package: a host hands it its set through the STTSet interface.
package providerpolicy

import (
	"fmt"

	"github.com/kombifyio/SpeechKit/app/internal/config"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// STTSet is an STT provider set a host assembled and has not constructed
// yet, named by provider-policy adapter id. The policy never needs to know
// which provider packages a host links: the host's set lists what it carries
// (models.STTRouterSpec, or the batteries' enabled-provider set) and drops
// what the policy refuses.
type STTSet interface {
	// Adapters lists the adapter ids the set carries, in assembly order.
	Adapters() []string
	// Drop removes the provider of adapter.
	Drop(adapter string)
}

// EnforceSTT drops every provider in set that cfg's policy refuses and
// returns one log line per refused provider, naming the stable reason id.
// It runs after the host filled set and before anything is constructed.
func EnforceSTT(cfg *config.Config, set STTSet) []string {
	if cfg == nil || set == nil {
		return nil
	}
	var notes []string
	for _, adapter := range set.Adapters() {
		if ok, reason := cfg.ProviderConstructionAllowed(adapter, provideropts.ModalitySTT); !ok {
			set.Drop(adapter)
			notes = append(notes, refusedNote(adapter, reason))
		}
	}
	return notes
}

// AllowSTT reports whether cfg's policy admits an already constructed STT
// provider (by its Name), with the reason id when it does not. Hosts that
// build providers one by one drop a refused one before it reaches a router.
func AllowSTT(cfg *config.Config, p stt.STTProvider) (bool, string) {
	if p == nil {
		return false, ""
	}
	return cfg.ProviderConstructionAllowed(p.Name(), provideropts.ModalitySTT)
}

// FilterSTT keeps the providers AllowSTT admits, in order.
func FilterSTT(cfg *config.Config, providers []stt.STTProvider) []stt.STTProvider {
	kept := make([]stt.STTProvider, 0, len(providers))
	for _, p := range providers {
		if ok, _ := AllowSTT(cfg, p); ok {
			kept = append(kept, p)
		}
	}
	return kept
}

func refusedNote(adapter, reason string) string {
	return fmt.Sprintf("Policy: STT provider %s not constructed [%s]", adapter, reason)
}
