// Package ttsroute exposes the shared voice-output profile routing contract.
//
// Stability: Beta — API-checked; may change with a changelog callout.
package ttsroute

import core "github.com/kombifyio/SpeechKit/pkg/speechkit/internal/ttsroute"

// PreferredProvider maps a voice-output profile to its configured provider.
func PreferredProvider(profileID string) string { return core.PreferredProvider(profileID) }
