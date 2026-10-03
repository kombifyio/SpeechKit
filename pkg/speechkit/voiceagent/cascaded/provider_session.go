package cascaded

import (
	"context"
	"errors"
	"fmt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/internal/logutil"
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
		logutil.Resolve(p.logger).Info("cascaded: TTS not configured; sessions will be text-only")
	}

	p.mu.Lock()
	select {
	case <-p.closedCh:
		p.mu.Unlock()
		return ErrClosed
	default:
	}
	if p.sessionCtx != nil {
		p.mu.Unlock()
		return errors.New("cascaded: session already connected")
	}
	ctx, sessionCancel := context.WithCancel(ctx)
	p.sessionCancel = sessionCancel
	p.sessionCtx = ctx
	p.locale = firstNonEmpty(cfg.Locale, "en")
	p.voice = cfg.Voice
	p.systemPrompt = firstNonEmpty(cfg.SystemPrompt, "")
	p.refinement = cfg.RefinementPrompt
	p.speaker = cfg.Speaker.Normalized()
	p.mu.Unlock()

	if err := p.ensureSpeakerStream(ctx); err != nil {
		logutil.Resolve(p.logger).Warn("cascaded: speaker stream unavailable", "code", turnFailureCode(err))
	}

	go func() {
		defer sessionCancel()
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
	logutil.Resolve(p.logger).Error("cascaded: goroutine panic recovered",
		"goroutine", name,
		"stack", string(debug.Stack()),
	)
	p.emitError("internal_panic")
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
		logutil.Resolve(p.logger).Warn("cascaded: speaker stream unavailable after config update", "err", speakerErr)
	}
	return nil
}

// SendAudio appends PCM to the current turn buffer and triggers
// processing when a silence boundary is reached.
func (p *Provider) SendAudio(chunk []byte) error {
	ctx, err := p.activeContext()
	if err != nil {
		return err
	}
	if len(chunk) == 0 {
		return nil
	}
	p.mu.Lock()
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		return err
	}
	p.buffer = append(p.buffer, chunk...)
	if rms := ChunkRMS(chunk); rms > p.cfg.SilenceRMSThreshold {
		p.lastVoiceAt = time.Now()
	}
	p.maybeTriggerLocked()
	p.mu.Unlock()

	if stream := p.currentSpeakerStream(); stream != nil {
		if err := stream.SendAudio(ctx, chunk); err != nil {
			logutil.Resolve(p.logger).Warn("cascaded: speaker stream audio send failed", "code", turnFailureCode(err))
		}
	}
	return nil
}

// SendAudioStreamEnd forces the current buffer to be treated as a
// complete turn, even if silence has not yet been detected.
func (p *Provider) SendAudioStreamEnd() error {
	ctx, err := p.activeContext()
	if err != nil {
		return err
	}
	p.mu.Lock()
	if len(p.buffer) == 0 {
		p.mu.Unlock()
		return nil
	}
	p.fire()
	p.mu.Unlock()
	if stream := p.currentSpeakerStream(); stream != nil {
		if err := stream.EndAudio(ctx); err != nil {
			logutil.Resolve(p.logger).Warn("cascaded: speaker stream end failed", "code", turnFailureCode(err))
		}
	}
	return nil
}

// SendText injects a text turn (skipping STT). Useful for testing and
// for clients that already have a transcript from their own STT.
func (p *Provider) SendText(text string) error {
	ctx, err := p.activeContext()
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	go func() {
		defer p.recoverGoroutine("runTurn")
		if err := p.runTurn(ctx, text, false); err != nil && ctx.Err() == nil {
			p.emitError(turnFailureCode(err))
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
		close(p.closedCh)
		p.mu.Lock()
		if p.sessionCancel != nil {
			p.sessionCancel()
		}
		p.buffer = nil
		p.history = nil
		p.mu.Unlock()
		p.closeSpeakerStream()
	})
	return nil
}

func (p *Provider) activeContext() (context.Context, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.closedCh:
		return nil, ErrClosed
	default:
	}
	if p.sessionCtx == nil {
		return nil, ErrNotConfigured
	}
	if err := p.sessionCtx.Err(); err != nil {
		return nil, err
	}
	return p.sessionCtx, nil
}

// Name returns the provider identifier used in logs and observability.
func (p *Provider) Name() string { return "cascaded" }
