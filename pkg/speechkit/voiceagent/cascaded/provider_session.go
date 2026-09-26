package cascaded

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"
)

// Connect validates that the required dependencies are satisfied and
// starts the processor loop. Unlike a native realtime adapter, no external handshake
// happens.
func (p *Provider) Connect(ctx context.Context, cfg SessionConfig) error {
	if p.stt == nil {
		return fmt.Errorf("cascaded: %w: STT router", ErrNotConfigured)
	}
	if p.agent == nil {
		return fmt.Errorf("cascaded: %w: Agent flow (no LLM models available)", ErrNotConfigured)
	}
	if p.tts == nil {
		// TTS is optional - without it we still return OutputTranscript
		// text frames so the client can render subtitles or speak via
		// its own TTS stack.
		slog.Info("cascaded: TTS not configured; sessions will be text-only")
	}

	p.mu.Lock()
	p.locale = firstNonEmpty(cfg.Locale, "en")
	p.voice = cfg.Voice
	p.systemPrompt = firstNonEmpty(cfg.SystemPrompt, "")
	p.refinement = cfg.RefinementPrompt
	p.speaker = cfg.Speaker.Normalized()
	p.mu.Unlock()

	if err := p.ensureSpeakerStream(ctx); err != nil {
		slog.Warn("cascaded: speaker stream unavailable", "err", err)
	}

	go func() {
		defer p.recoverGoroutine("processorLoop")
		p.processorLoop(ctx)
	}()
	return nil
}

// recoverGoroutine converts a panic in a spawned provider goroutine into a
// logged error plus a client-visible error message, instead of crashing the
// whole process. These goroutines run outside any HTTP handler, so the
// server's Recover middleware cannot protect them.
func (p *Provider) recoverGoroutine(name string) {
	rec := recover()
	if rec == nil {
		return
	}
	slog.Error("cascaded: goroutine panic recovered",
		"goroutine", name,
		"err", rec,
		"stack", string(debug.Stack()),
	)
	p.emitError("internal_panic", "internal error during processing")
}

// UpdateInstructions changes future-turn host instructions without
// creating a synthetic user turn.
func (p *Provider) UpdateInstructions(ctx context.Context, cfg SessionConfig) error {
	p.mu.Lock()
	if cfg.Locale != "" {
		p.locale = cfg.Locale
	}
	if cfg.Voice != "" {
		p.voice = cfg.Voice
	}
	if cfg.SystemPrompt != "" {
		p.systemPrompt = cfg.SystemPrompt
	}
	if cfg.RefinementPrompt != "" {
		p.refinement = cfg.RefinementPrompt
	}
	if cfg.Speaker.WantsDiarization() || cfg.Speaker.Enabled {
		p.speaker = cfg.Speaker.Normalized()
	}
	p.mu.Unlock()

	speakerErr := p.ensureSpeakerStream(ctx)
	if speakerErr != nil {
		slog.Warn("cascaded: speaker stream unavailable after config update", "err", speakerErr)
	}
	return nil
}

// SendAudio appends PCM to the current turn buffer and triggers
// processing when a silence boundary is reached.
func (p *Provider) SendAudio(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	p.mu.Lock()
	p.buffer = append(p.buffer, chunk...)
	if rms := ChunkRMS(chunk); rms > p.cfg.SilenceRMSThreshold {
		p.lastVoiceAt = time.Now()
	}
	p.maybeTriggerLocked()
	p.mu.Unlock()

	if stream := p.currentSpeakerStream(); stream != nil {
		if err := stream.SendAudio(context.Background(), chunk); err != nil {
			slog.Warn("cascaded: speaker stream audio send failed", "err", err)
		}
	}
	return nil
}

// SendAudioStreamEnd forces the current buffer to be treated as a
// complete turn, even if silence has not yet been detected.
func (p *Provider) SendAudioStreamEnd() error {
	p.mu.Lock()
	if len(p.buffer) == 0 {
		p.mu.Unlock()
		return nil
	}
	p.fire()
	p.mu.Unlock()
	if stream := p.currentSpeakerStream(); stream != nil {
		if err := stream.EndAudio(context.Background()); err != nil {
			slog.Warn("cascaded: speaker stream end failed", "err", err)
		}
	}
	return nil
}

// SendText injects a text turn (skipping STT). Useful for testing and
// for clients that already have a transcript from their own STT.
func (p *Provider) SendText(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	go func() {
		defer p.recoverGoroutine("runTurn")
		if err := p.runTurn(context.Background(), text, false); err != nil {
			p.emitError("turn_failed", err.Error())
		}
	}()
	return nil
}

// Receive blocks until the next message is ready or the provider closes.
func (p *Provider) Receive(ctx context.Context) (*Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.closedCh:
		return nil, ErrClosed
	case msg, ok := <-p.messages:
		if !ok {
			return nil, ErrClosed
		}
		return msg, nil
	}
}

// Close stops the processor loop and drains any pending buffer.
func (p *Provider) Close() error {
	p.closeOnce.Do(func() {
		p.closeSpeakerStream()
		close(p.closedCh)
	})
	return nil
}

// Name returns the provider identifier used in logs and observability.
func (p *Provider) Name() string { return "cascaded" }
