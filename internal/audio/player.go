package audio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	mp3 "github.com/hajimehoshi/go-mp3"

	"github.com/ebitengine/oto/v3"
)

// Player plays audio through the system's default output device.
type Player struct {
	mu        sync.Mutex
	otoCtx    *oto.Context
	current   *oto.Player
	playing   atomic.Bool
	onDone    func()
	cancelled atomic.Bool
}

var (
	playerOnce sync.Once
	sharedCtx  *oto.Context
	initErr    error
)

// initOtoContext lazily initializes the shared oto context.
// oto requires exactly one context per process.
func initOtoContext(sampleRate, channels int) (*oto.Context, error) {
	playerOnce.Do(func() {
		opts := &oto.NewContextOptions{
			SampleRate:   sampleRate,
			ChannelCount: channels,
			Format:       oto.FormatSignedInt16LE,
		}
		var ready chan struct{}
		sharedCtx, ready, initErr = oto.NewContext(opts)
		if initErr != nil {
			initErr = fmt.Errorf("oto init: %w", initErr)
			return
		}
		<-ready
	})
	return sharedCtx, initErr
}

// NewPlayer creates an audio player for TTS output.
// Call once at app startup; reuse for all playback.
func NewPlayer() (*Player, error) {
	// Default: 24kHz mono 16-bit (OpenAI TTS native format).
	ctx, err := initOtoContext(24000, 1)
	if err != nil {
		return nil, err
	}
	return &Player{otoCtx: ctx}, nil
}

// PlayMP3 decodes and plays MP3 audio data.
// Blocks until playback completes or Stop() is called.
func (p *Player) PlayMP3(ctx context.Context, data []byte) error {
	decoder, err := mp3.NewDecoder(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("mp3 decode: %w", err)
	}

	return p.playStream(ctx, decoder, decoder.SampleRate())
}

// PlayPCM plays raw PCM audio (16-bit signed int, little-endian, mono).
// IMPORTANT: The oto context is initialized at 24kHz. Audio with a different
// sample rate will play at the wrong pitch/speed. Callers must resample to
// 24kHz before calling this method, or use PlayMP3 which handles decoding.
func (p *Player) PlayPCM(ctx context.Context, data []byte, sampleRate int) error {
	if sampleRate != 0 && sampleRate != 24000 {
		log.Printf("audio player: WARNING: PCM sample rate %dHz does not match oto context (24000Hz) — audio may play at wrong pitch", sampleRate)
	}
	return p.playStream(ctx, bytes.NewReader(data), sampleRate)
}

// playStream plays audio from a reader through oto.
func (p *Player) playStream(ctx context.Context, reader io.Reader, sampleRate int) error {
	p.Stop() // Stop any current playback.

	p.mu.Lock()
	p.cancelled.Store(false)
	player := p.otoCtx.NewPlayer(reader)
	p.current = player
	p.playing.Store(true)
	p.mu.Unlock()

	player.Play()

	// Wait for playback to complete or context cancellation.
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for player.IsPlaying() {
			if p.cancelled.Load() {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	select {
	case <-done:
		// Playback finished naturally.
	case <-ctx.Done():
		p.stopCurrent()
		return ctx.Err()
	}

	p.mu.Lock()
	p.playing.Store(false)
	p.current = nil
	onDone := p.onDone
	p.mu.Unlock()

	if onDone != nil {
		onDone()
	}

	return nil
}

// Stop immediately stops current playback (for barge-in support).
func (p *Player) Stop() {
	p.cancelled.Store(true)
	p.stopCurrent()
}

func (p *Player) stopCurrent() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current != nil {
		if err := p.current.Close(); err != nil {
			log.Printf("audio player: close error: %v", err)
		}
		p.current = nil
		p.playing.Store(false)
	}
}

// IsPlaying returns true if audio is currently being played.
func (p *Player) IsPlaying() bool {
	return p.playing.Load()
}

// OnFinished sets a callback that fires when playback completes naturally
// (not when stopped via Stop()).
func (p *Player) OnFinished(fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onDone = fn
}

// Close releases audio resources. Call on app shutdown.
func (p *Player) Close() {
	p.Stop()
}
