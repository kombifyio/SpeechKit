package cascaded

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// Speaker-stream reconnect tuning. Vars (not consts) so tests can shorten the
// backoff without waiting real seconds.
var (
	speakerReconnectInitialBackoff = 250 * time.Millisecond
	speakerReconnectMaxBackoff     = 10 * time.Second
)

const speakerReconnectMaxAttempts = 6

// speakerStreamAudioFormat is the PCM format the cascaded provider feeds to the
// speaker streamer (matches the 16 kHz mono capture path).
func speakerStreamAudioFormat() speaker.AudioFormat {
	return speaker.AudioFormat{
		Encoding:     speaker.AudioEncodingLinear16,
		SampleRateHz: 16000,
		Channels:     1,
	}
}

func (p *Provider) ensureSpeakerStream(ctx context.Context) error {
	opts := p.currentSpeakerOptions()
	if !opts.PreferStreaming || !opts.WantsDiarization() || p.speakerStreamer == nil {
		return nil
	}
	p.speakerStreamMu.Lock()
	if p.speakerStream != nil {
		p.speakerStreamMu.Unlock()
		return nil
	}
	p.speakerStreamMu.Unlock()

	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := p.speakerStreamer.StartSpeakerStream(streamCtx, opts, speakerStreamAudioFormat())
	if err != nil {
		cancel()
		return err
	}

	p.speakerStreamMu.Lock()
	if p.speakerStream != nil {
		p.speakerStreamMu.Unlock()
		cancel()
		_ = stream.Close()
		return nil
	}
	p.speakerStream = stream
	p.speakerStreamCancel = cancel
	p.speakerStreamMu.Unlock()

	go func() {
		defer p.recoverGoroutine("speakerStreamLoop")
		p.speakerStreamLoop(streamCtx, stream)
	}()
	return nil
}

func (p *Provider) currentSpeakerOptions() speaker.Options {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.speaker
}

func (p *Provider) currentSpeakerStream() speaker.SpeakerStream {
	p.speakerStreamMu.Lock()
	defer p.speakerStreamMu.Unlock()
	return p.speakerStream
}

func (p *Provider) closeSpeakerStream() {
	p.speakerStreamMu.Lock()
	stream := p.speakerStream
	cancel := p.speakerStreamCancel
	p.speakerStream = nil
	p.speakerStreamCancel = nil
	p.speakerStreamMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if stream != nil {
		_ = stream.Close()
	}
}

func (p *Provider) speakerStreamLoop(ctx context.Context, stream speaker.SpeakerStream) {
	for {
		frame, err := stream.Receive(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
				return
			}
			select {
			case <-p.closedCh:
				return
			case <-ctx.Done():
				return
			default:
			}
			// Transient stream failure (e.g. a dropped WebSocket). A multi-hour
			// session must not lose diarization for good over a single blip, so
			// reconnect with capped backoff and resume on a fresh stream.
			slog.Warn("cascaded: speaker stream receive failed; reconnecting", "err", err)
			next := p.reconnectSpeakerStream(ctx, stream)
			if next == nil {
				return
			}
			stream = next
			continue
		}
		if msg := inputTranscriptMessageFromSpeakerFrame(frame); msg != nil {
			select {
			case p.messages <- msg:
			case <-p.closedCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}
}

// reconnectSpeakerStream replaces a dropped speaker stream with a fresh one,
// retrying with capped exponential backoff. It returns nil when the session is
// shutting down, the lifecycle already replaced or closed the stream, or every
// reconnect attempt failed.
func (p *Provider) reconnectSpeakerStream(ctx context.Context, dead speaker.SpeakerStream) speaker.SpeakerStream {
	// Only the goroutine that still owns the active stream may reconnect it; if
	// the lifecycle swapped or closed it, stand down.
	p.speakerStreamMu.Lock()
	owns := p.speakerStream == dead
	p.speakerStreamMu.Unlock()
	if !owns {
		return nil
	}
	_ = dead.Close()

	backoff := speakerReconnectInitialBackoff
	for attempt := 1; attempt <= speakerReconnectMaxAttempts; attempt++ {
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil
		case <-p.closedCh:
			return nil
		}
		p.speakerStreamMu.Lock()
		owns = p.speakerStream == dead
		p.speakerStreamMu.Unlock()
		if !owns {
			return nil
		}
		fresh, err := p.speakerStreamer.StartSpeakerStream(ctx, p.currentSpeakerOptions(), speakerStreamAudioFormat())
		if err != nil {
			slog.Warn("cascaded: speaker stream reconnect attempt failed", "attempt", attempt, "err", err)
			backoff = min(backoff*2, speakerReconnectMaxBackoff)
			continue
		}
		p.speakerStreamMu.Lock()
		if p.speakerStream != dead {
			p.speakerStreamMu.Unlock()
			_ = fresh.Close()
			return nil
		}
		p.speakerStream = fresh
		p.speakerStreamMu.Unlock()
		slog.Info("cascaded: speaker stream reconnected", "attempt", attempt)
		return fresh
	}
	slog.Warn("cascaded: speaker stream reconnect exhausted; diarization stopped",
		"attempts", speakerReconnectMaxAttempts)
	return nil
}
