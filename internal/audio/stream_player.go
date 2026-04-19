package audio

import (
	"context"
	"io"
	"strings"
	"sync"

	"github.com/ebitengine/oto/v3"
)

const (
	voiceAgentOutputSampleRate = 24000
	voiceAgentOutputChannels   = 1
)

type streamPlaybackBackend interface {
	Start(context.Context)
	WriteChunk([]byte)
	StopAndDrain()
	IsActive() bool
	Close()
}

type streamPlayerFactoryFunc func(outputDeviceID string) (streamPlaybackBackend, error)

var streamPlayerFactory streamPlayerFactoryFunc = newOtoStreamPlayer

// StreamPlayer plays a continuous stream of PCM audio chunks through a single
// playback backend. Unlike Player.PlayPCM which stops previous playback on each
// call, StreamPlayer buffers chunks and plays them sequentially. Designed for
// real-time voice agent audio output (Gemini Live, OpenAI Realtime).
type StreamPlayer struct {
	backend streamPlaybackBackend
}

// NewStreamPlayer creates a StreamPlayer using the system default output device.
func NewStreamPlayer() (*StreamPlayer, error) {
	return NewStreamPlayerWithOutputDevice("")
}

// NewStreamPlayerWithOutputDevice creates a StreamPlayer for the selected output device.
// An empty device ID uses the system default output device.
func NewStreamPlayerWithOutputDevice(outputDeviceID string) (*StreamPlayer, error) {
	backend, err := streamPlayerFactory(strings.TrimSpace(outputDeviceID))
	if err != nil {
		return nil, err
	}
	return &StreamPlayer{backend: backend}, nil
}

func (sp *StreamPlayer) Start(ctx context.Context) {
	if sp == nil || sp.backend == nil {
		return
	}
	sp.backend.Start(ctx)
}

func (sp *StreamPlayer) WriteChunk(chunk []byte) {
	if sp == nil || sp.backend == nil {
		return
	}
	sp.backend.WriteChunk(chunk)
}

func (sp *StreamPlayer) StopAndDrain() {
	if sp == nil || sp.backend == nil {
		return
	}
	sp.backend.StopAndDrain()
}

func (sp *StreamPlayer) IsActive() bool {
	if sp == nil || sp.backend == nil {
		return false
	}
	return sp.backend.IsActive()
}

func (sp *StreamPlayer) Close() {
	if sp == nil || sp.backend == nil {
		return
	}
	sp.backend.Close()
}

type otoStreamPlayer struct {
	mu     sync.Mutex
	otoCtx *oto.Context
	player *oto.Player
	pipe   *streamPipe
	active bool
	cancel context.CancelFunc
	doneCh chan struct{}
}

// newOtoStreamPlayer creates a playback backend that uses the shared oto context.
// The oto context must already be initialized (by NewPlayer or initOtoContext).
func newOtoStreamPlayer(_ string) (streamPlaybackBackend, error) {
	ctx, err := initOtoContext(voiceAgentOutputSampleRate, voiceAgentOutputChannels)
	if err != nil {
		return nil, err
	}
	return &otoStreamPlayer{otoCtx: ctx}, nil
}

// Start begins a new streaming playback session.
// Any previous session is stopped first.
func (sp *otoStreamPlayer) Start(ctx context.Context) {
	sp.StopAndDrain()

	sp.mu.Lock()
	pipe := newStreamPipe()
	player := sp.otoCtx.NewPlayer(pipe)
	playCtx, cancel := context.WithCancel(ctx)
	sp.pipe = pipe
	sp.player = player
	sp.cancel = cancel
	sp.active = true
	sp.doneCh = make(chan struct{})
	sp.mu.Unlock()

	player.Play()

	// Monitor context cancellation to stop playback.
	go func() {
		defer close(sp.doneCh)
		<-playCtx.Done()
		sp.mu.Lock()
		sp.active = false
		if sp.pipe != nil {
			sp.pipe.Close()
		}
		sp.mu.Unlock()
	}()
}

// WriteChunk appends a PCM audio chunk to the streaming buffer.
// Safe to call from any goroutine. No-op if not active.
func (sp *otoStreamPlayer) WriteChunk(chunk []byte) {
	sp.mu.Lock()
	pipe := sp.pipe
	active := sp.active
	sp.mu.Unlock()
	if !active || pipe == nil || len(chunk) == 0 {
		return
	}
	pipe.Write(chunk)
}

// StopAndDrain stops playback immediately (barge-in).
// Blocks briefly until the player goroutine exits.
func (sp *otoStreamPlayer) StopAndDrain() {
	sp.mu.Lock()
	cancel := sp.cancel
	pipe := sp.pipe
	doneCh := sp.doneCh
	sp.active = false
	sp.player = nil
	sp.pipe = nil
	sp.cancel = nil
	sp.mu.Unlock()

	if pipe != nil {
		pipe.Close()
	}
	if cancel != nil {
		cancel()
	}
	if doneCh != nil {
		<-doneCh
	}
}

// IsActive returns true if a streaming session is in progress.
func (sp *otoStreamPlayer) IsActive() bool {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.active
}

// Close releases resources.
func (sp *otoStreamPlayer) Close() {
	sp.StopAndDrain()
}

// streamPipe is a thread-safe pipe that bridges Write (producer) and Read (oto consumer).
type streamPipe struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

func newStreamPipe() *streamPipe {
	p := &streamPipe{}
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *streamPipe) Write(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.buf = append(p.buf, data...)
	p.cond.Signal()
}

func (p *streamPipe) Read(dst []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for len(p.buf) == 0 && !p.closed {
		p.cond.Wait()
	}
	if len(p.buf) == 0 && p.closed {
		return 0, io.EOF
	}

	n := copy(dst, p.buf)
	p.buf = p.buf[n:]
	return n, nil
}

func (p *streamPipe) ReadAvailable(dst []byte) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(dst) == 0 || len(p.buf) == 0 {
		return 0
	}

	n := copy(dst, p.buf)
	p.buf = p.buf[n:]
	return n
}

func (p *streamPipe) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	p.cond.Broadcast()
}

// Ensure streamPipe implements io.Reader.
var _ io.Reader = (*streamPipe)(nil)
