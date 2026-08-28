// Package assemblyai is the AssemblyAI Voice Agent provider.
//
// In v0.64.0 the identifiers below alias the implementation that still lives
// in pkg/speechkit/voiceagent/live. v0.65.0 moves the implementation here and
// deletes the old names, so code written against this package needs no
// further change.
package assemblyai

//lint:file-ignore SA1019 This package exists to alias the names deprecated in
// pkg/speechkit/voiceagent/live; flagging its own aliases would defeat the
// migration window.

import "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"

// Provider speaks the AssemblyAI Voice Agent protocol.
type Provider = live.AssemblyAILive

// New returns an unconnected AssemblyAI Voice Agent provider.
func New() *Provider { return live.NewAssemblyAILive() }
