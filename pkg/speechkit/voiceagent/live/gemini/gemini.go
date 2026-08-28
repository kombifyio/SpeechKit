// Package gemini is the Gemini Live realtime Voice Agent provider.
//
// In v0.64.0 the identifiers below alias the implementation that still lives
// in pkg/speechkit/voiceagent/live. v0.65.0 moves the implementation here and
// deletes the old names, so code written against this package needs no
// further change.
package gemini

//lint:file-ignore SA1019 This package exists to alias the names deprecated in
// pkg/speechkit/voiceagent/live; flagging its own aliases would defeat the
// migration window.

import "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"

// Provider speaks the Gemini Live audio-to-audio protocol.
type Provider = live.GeminiLive

// New returns an unconnected Gemini Live provider.
func New() *Provider { return live.NewGeminiLive() }
