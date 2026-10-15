//go:build linux

package voiceagent

import (
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

// DownlinkSampleRate is the rate of server-to-client Voice Agent audio frames
// (raw S16LE mono, see protocol.go).
const DownlinkSampleRate = cascaded.OutputSampleRate

// NewDownlinkTTS adapts a TTS engine to the Voice Agent wire. Clients play
// every binary frame as 24 kHz S16LE mono PCM, so the synthesized payload is
// requested as WAV (its header carries the true rate) and converted to that
// shape whatever the selected provider returns. A payload that cannot be
// converted fails the turn instead of reaching the speaker as noise.
func NewDownlinkTTS(inner CascadedTTS) CascadedTTS {
	return cascaded.NewPCMOutputTTS(inner)
}
