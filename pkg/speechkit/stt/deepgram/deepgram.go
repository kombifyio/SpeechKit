// Package deepgram is the Deepgram provider for SpeechKit: batch transcription,
// speaker streaming, live dictation, and the Flux turn stream.
//
// In v0.64.0 the identifiers below alias the implementation that still lives
// in pkg/speechkit/stt. v0.65.0 moves the implementation here and deletes the
// old names, so code written against this package needs no further change.
package deepgram

//lint:file-ignore SA1019 This package exists to alias the names deprecated in
// pkg/speechkit/stt; flagging its own aliases would defeat the migration window.

import "github.com/kombifyio/SpeechKit/pkg/speechkit/stt"

// Provider transcribes through the Deepgram Listen API.
type Provider = stt.DeepgramProvider

// Options are the Deepgram Listen options a host may apply to a Provider.
type Options = stt.DeepgramOptions

// Flux turn-stream types. Flux reports complete conversational turns instead
// of raw partials, so a caller does not have to segment the stream itself.
type (
	FluxStreamOptions = stt.FluxStreamOptions
	FluxWord          = stt.FluxWord
	FluxTurn          = stt.FluxTurn
	FluxTurnStream    = stt.FluxTurnStream
)

// FluxAudioChunk is the audio chunk duration the Flux stream expects.
const FluxAudioChunk = stt.FluxAudioChunk

// New returns a Deepgram provider for apiKey. An empty model uses the
// provider default.
func New(apiKey, model string) *Provider { return stt.NewDeepgramProvider(apiKey, model) }
