// Wire protocol for the streaming Dictation WebSocket
// (GET /v1/dictation/stream/sessions/{id}/ws).
//
// Control frames are JSON text messages with a required "type" field. Audio
// frames are binary messages containing raw PCM (default 16 kHz S16 mono,
// client → server only — the server never sends audio on this surface).
// The first frame a client sends must be a "start" message; after a
// "segment_done" the client may send another "start" to begin the next
// segment on the same socket (a keyboard keeps one warm connection across
// mic presses).
//
// This file is deliberately free of build tags (mirroring
// internal/server/wyoming/wire.go) so the frame codecs are natively testable
// on any development host; the handler and adapter stay `//go:build linux`.
package dictation

import (
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// Client-to-server message types.
const (
	StreamMsgStart    = "start"
	StreamMsgFinalize = "finalize"
	StreamMsgStop     = "stop"
	StreamMsgPing     = "ping"
)

// Server-to-client message types.
const (
	StreamMsgReady       = "ready"
	StreamMsgTranscript  = "transcript"
	StreamMsgSegmentDone = "segment_done"
	StreamMsgError       = "error"
	StreamMsgSessionEnd  = "session_end"
	StreamMsgPong        = "pong"
)

// Stable error codes emitted on StreamErrorFrame. Clients switch on these,
// never on the human-readable message.
const (
	StreamErrStartRequired        = "start_required"
	StreamErrSegmentActive        = "segment_active"
	StreamErrNoActiveSegment      = "no_active_segment"
	StreamErrStreamingUnavailable = "streaming_unavailable"
	StreamErrProviderStreamFailed = "provider_stream_failed"
	StreamErrProviderStreamClosed = "provider_stream_closed"
	StreamErrFormatUnsupported    = "format_unsupported"
	StreamErrInvalidFrame         = "invalid_frame"
)

// Session end reasons carried on StreamSessionEndFrame.
const (
	StreamEndReasonClient      = "client"
	StreamEndReasonIdle        = "idle"
	StreamEndReasonMaxDuration = "max_duration"
	StreamEndReasonMaxAudio    = "max_audio"
	StreamEndReasonError       = "error"
	StreamEndReasonShutdown    = "shutdown"
)

// StreamStartFrame is the mandatory first client frame of every segment. It
// maps 1:1 onto the kernel's speechkit.DictationStreamOptions plus the PCM
// input format.
type StreamStartFrame struct {
	Type              string `json:"type"` // must be "start"
	Language          string `json:"language,omitempty"`
	Model             string `json:"model,omitempty"`
	ProviderProfileID string `json:"provider_profile_id,omitempty"`
	// InterimResults defaults to true when omitted — live partials are the
	// point of this endpoint. Send false explicitly for finals-only.
	InterimResults *bool              `json:"interim_results,omitempty"`
	EndpointingMs  int                `json:"endpointing_ms,omitempty"`
	TurnDetection  bool               `json:"turn_detection,omitempty"`
	Keyterms       []string           `json:"keyterms,omitempty"`
	PromptHint     string             `json:"prompt_hint,omitempty"`
	Diarization    bool               `json:"diarization,omitempty"`
	Format         *StreamAudioFormat `json:"format,omitempty"`
}

// StreamAudioFormat describes the binary PCM frames the client will send.
// Zero values normalize to SpeechKit's canonical 16 kHz S16 mono.
type StreamAudioFormat struct {
	Encoding     string `json:"encoding,omitempty"`       // "linear16" (default) | "pcm16"
	SampleRateHz int    `json:"sample_rate_hz,omitempty"` // default 16000
	Channels     int    `json:"channels,omitempty"`       // default 1
}

// Options converts the start frame into kernel DictationStreamOptions.
func (f StreamStartFrame) Options() speechkit.DictationStreamOptions {
	interim := true
	if f.InterimResults != nil {
		interim = *f.InterimResults
	}
	return speechkit.DictationStreamOptions{
		ProviderProfileID: f.ProviderProfileID,
		Language:          f.Language,
		Model:             f.Model,
		InterimResults:    interim,
		EndpointingMs:     f.EndpointingMs,
		TurnDetection:     f.TurnDetection,
		Keyterms:          append([]string(nil), f.Keyterms...),
		PromptHint:        f.PromptHint,
		Diarization:       f.Diarization,
	}
}

// AudioFormat converts the start frame's format block into the kernel's
// speaker.AudioFormat, applying canonical defaults.
func (f StreamStartFrame) AudioFormat() speaker.AudioFormat {
	format := speaker.AudioFormat{}
	if f.Format != nil {
		format.Encoding = speaker.AudioEncoding(f.Format.Encoding)
		format.SampleRateHz = f.Format.SampleRateHz
		format.Channels = f.Format.Channels
	}
	return format.Normalized()
}

// ValidFormat reports whether the requested PCM format is one the adapter
// accepts, returning a stable reason string when it is not.
func (f StreamStartFrame) ValidFormat() (ok bool, reason string) {
	if f.Format == nil {
		return true, ""
	}
	switch speaker.AudioEncoding(f.Format.Encoding) {
	case "", speaker.AudioEncodingLinear16, speaker.AudioEncodingPCM16:
	default:
		return false, "encoding must be linear16 or pcm16"
	}
	if f.Format.SampleRateHz != 0 && (f.Format.SampleRateHz < 8000 || f.Format.SampleRateHz > 48000) {
		return false, "sample_rate_hz must be between 8000 and 48000"
	}
	if f.Format.Channels != 0 && (f.Format.Channels < 1 || f.Format.Channels > 2) {
		return false, "channels must be 1 or 2"
	}
	return true, ""
}

// ── server → client ─────────────────────────────────────────────────────────

// StreamReadyFrame acknowledges a start frame: the provider stream is up and
// binary PCM may flow.
type StreamReadyFrame struct {
	Type      string `json:"type"` // "ready"
	SegmentID uint64 `json:"segment_id"`
}

// StreamTranscriptFrame carries a draft (done=false) or final (done=true)
// transcript for the active segment. Drafts may update UI state; only finals
// should be committed to the target text field.
type StreamTranscriptFrame struct {
	Type       string                     `json:"type"` // "transcript"
	SegmentID  uint64                     `json:"segment_id"`
	Sequence   int64                      `json:"sequence,omitempty"`
	Text       string                     `json:"text"`
	Done       bool                       `json:"done"`
	Language   string                     `json:"language,omitempty"`
	Confidence float64                    `json:"confidence,omitempty"`
	Provider   string                     `json:"provider,omitempty"`
	Model      string                     `json:"model,omitempty"`
	Words      []StreamWord               `json:"words,omitempty"`
	Speakers   *speaker.DiarizationResult `json:"speakers,omitempty"`
}

// StreamWord is the word-level confidence entry on transcript frames. It
// mirrors speechkit.WordConfidence with stable snake_case JSON names.
type StreamWord struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence,omitempty"`
	StartMs    int64   `json:"start_ms,omitempty"`
	EndMs      int64   `json:"end_ms,omitempty"`
}

// StreamSegmentDoneFrame closes a segment: the provider stream is flushed and
// no further transcript frames will arrive for this segment_id. The client
// may send the next "start".
type StreamSegmentDoneFrame struct {
	Type      string `json:"type"` // "segment_done"
	SegmentID uint64 `json:"segment_id"`
}

// StreamErrorFrame reports a recoverable protocol or provider error. Unless a
// session_end follows, the socket stays open and the client may retry with a
// new "start" (or fall back to POST /v1/dictation/transcribe).
type StreamErrorFrame struct {
	Type    string `json:"type"` // "error"
	Code    string `json:"code"`
	Message string `json:"message"`
}

// StreamSessionEndFrame is terminal; the server closes the socket after
// sending it.
type StreamSessionEndFrame struct {
	Type   string `json:"type"` // "session_end"
	Reason string `json:"reason"`
}

// StreamPongFrame answers a client ping.
type StreamPongFrame struct {
	Type string `json:"type"` // "pong"
}

// streamEnvelope peeks at the type of an incoming frame before full decode.
type streamEnvelope struct {
	Type string `json:"type"`
}

// streamWordsFromKernel converts kernel word confidences to wire words.
func streamWordsFromKernel(words []speechkit.WordConfidence) []StreamWord {
	if len(words) == 0 {
		return nil
	}
	out := make([]StreamWord, 0, len(words))
	for _, w := range words {
		out = append(out, StreamWord{
			Text:       w.Text,
			Confidence: w.Confidence,
			StartMs:    w.StartMs,
			EndMs:      w.EndMs,
		})
	}
	return out
}

// streamTranscriptFromEvent maps a kernel DictationStreamEvent onto the wire
// frame for the given segment.
func streamTranscriptFromEvent(segmentID uint64, event speechkit.DictationStreamEvent) StreamTranscriptFrame {
	return StreamTranscriptFrame{
		Type:       StreamMsgTranscript,
		SegmentID:  segmentID,
		Sequence:   event.Sequence,
		Text:       event.Text,
		Done:       event.IsFinal,
		Language:   event.Language,
		Confidence: event.Confidence,
		Provider:   event.Provider,
		Model:      event.Model,
		Words:      streamWordsFromKernel(event.Words),
		Speakers:   event.Speakers,
	}
}
