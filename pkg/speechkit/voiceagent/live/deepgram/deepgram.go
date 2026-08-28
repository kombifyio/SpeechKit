// Package deepgram is the Deepgram Voice Agent provider.
//
// In v0.64.0 the identifiers below alias the implementation that still lives
// in pkg/speechkit/voiceagent/live. v0.65.0 moves the implementation here and
// deletes the old names, so code written against this package needs no
// further change.
package deepgram

//lint:file-ignore SA1019 This package exists to alias the names deprecated in
// pkg/speechkit/voiceagent/live; flagging its own aliases would defeat the
// migration window.

import "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"

// Provider speaks the Deepgram Voice Agent protocol.
type Provider = live.DeepgramLive

// AudioSettings carries the input and output audio formats Deepgram
// negotiates for a session.
type AudioSettings = live.DeepgramAudioSettings

// New returns an unconnected Deepgram Voice Agent provider.
func New() *Provider { return live.NewDeepgramLive() }
