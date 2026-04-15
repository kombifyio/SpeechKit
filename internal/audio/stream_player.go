package audio

import (
	"context"
	"io"
	"sync"

	"github.com/ebitengine/oto/v3"
)

// StreamPlayer plays a continuous stream of PCM audio chunks through a single
// oto player instance. Unlike Player.PlayPCM which stops previous playback on
// each call, StreamPlayer buffers chunks and plays them sequentially.
// Designed for real-time voice agent audio output (Gemini Live, OpenAI Realtime).
type StreamPlayer struct {
	mu     sync.Mutex
	otoCtx *oto.Context
	player *oto.Player
	pipe   *streamPipe
	active bool
	cancel context.CancelFunc
	doneCh chan struct{}
	onDone func()
}

// NewStreamPlayer creates a StreamPlayer that uses the shared oto context.
// The oto context must already be initialized (by NewPlayer or initOtoContext).
func NewStreamPlayer() (*StreamPlayer, error) {
	ctx, err := initOtoContext(24000, 1)
	if err != nil {
		return nil, err
	}
	return &StreamPlayer{otoCtx: ctx}, nil
}

// Start begins a new streaming playback session.
// Any previous session is stopped first.
func (sp *StreamPlayer) Start(ctx context.Context) {
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
func (sp *StreamPlayer) WriteChunk(chunk []byte) {
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
func (sp *StreamPlayer) StopAndDrain() {
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
func (sp *StreamPlayer) IsActive() bool {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.active
}

// Close releases resources.
func (sp *StreamPlayer) Close() {
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
