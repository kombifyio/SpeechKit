package cascaded

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/internal/logutil"
)

// RecognitionStreamer opens live speech recognition for one spoken turn. It is
// the optional streaming counterpart of [STT]: audio is recognised while the
// user is still speaking, so the turn's transcript is ready the moment the
// user stops instead of after a batch upload of the whole utterance.
type RecognitionStreamer interface {
	OpenRecognition(ctx context.Context, locale string, onFinal func()) (Recognition, error)
}

// Recognition is one live recognition of a single turn. onFinal (given to
// OpenRecognition) runs, on any goroutine, whenever the recogniser has
// committed an utterance: it heard the speaker stop on its own, which is the
// signal the provider uses to end a turn before the plain silence timer.
type Recognition interface {
	// SendPCM adds 16 kHz mono signed 16-bit little-endian audio.
	SendPCM(ctx context.Context, pcm []byte) error
	// Finish ends the audio and returns the text of everything recognised for
	// the turn. It returns once the recogniser has flushed its tail or ctx ends.
	Finish(ctx context.Context) (string, error)
	// Close releases the recognition without waiting for a result.
	Close() error
}

const (
	// recognitionPreRollMs is the audio kept ahead of the first voiced chunk, so
	// the start of the first word is not clipped.
	recognitionPreRollMs = 400
	// recognitionFinishTimeout bounds how long the end of a turn waits for the
	// recogniser's tail before the batch recogniser takes over.
	recognitionFinishTimeout = 1500 * time.Millisecond
	// recognitionQueueChunks bounds the audio queued for a recognition that is
	// still connecting or slow; an overflow gives the turn to the batch path.
	recognitionQueueChunks = 1024
	// earlyCommitSilenceMs is the silence that, together with the recogniser's own
	// end-of-utterance signal, ends a turn before the plain silence timer.
	earlyCommitSilenceMs = 300
)

// liveTurn is the streaming recognition of the turn being spoken. Its chunk
// queue is written only with Provider.mu held and closed once, by the turn's
// owner, with the same lock.
type liveTurn struct {
	chunks   chan []byte
	worker   chan struct{} // closed when the worker returned
	cancel   context.CancelFunc
	mu       sync.Mutex
	rec      Recognition
	failed   bool
	endHeard bool // the recogniser committed an utterance after the last voiced chunk
}

func (t *liveTurn) fail() {
	t.mu.Lock()
	t.failed = true
	t.mu.Unlock()
}

// startLiveTurnLocked opens the streaming recognition at the first voiced chunk
// of a turn. It never blocks the audio path: the connection is made by a worker
// that feeds the queued audio once it is open.
func (p *Provider) startLiveTurnLocked(ctx context.Context, preRoll []byte) {
	turnCtx, cancel := context.WithCancel(ctx)
	turn := &liveTurn{chunks: make(chan []byte, recognitionQueueChunks), worker: make(chan struct{}), cancel: cancel}
	if len(preRoll) > 0 {
		turn.chunks <- preRoll
	}
	locale := p.locale
	p.liveTurn = turn
	go func() {
		defer close(turn.worker)
		defer p.recoverGoroutine("liveRecognition")
		rec, err := p.recognizer.OpenRecognition(turnCtx, locale, func() {
			turn.mu.Lock()
			turn.endHeard = true
			turn.mu.Unlock()
		})
		if err != nil {
			logutil.Resolve(p.logger).Warn("cascaded: streaming recognition unavailable; using batch recognition", "code", turnFailureCode(err))
			turn.fail()
			for range turn.chunks { // keep the audio path unblocked until the owner closes the queue
			}
			return
		}
		turn.mu.Lock()
		turn.rec = rec
		turn.mu.Unlock()
		for chunk := range turn.chunks {
			if err := rec.SendPCM(turnCtx, chunk); err != nil {
				logutil.Resolve(p.logger).Warn("cascaded: streaming recognition audio failed; using batch recognition", "code", turnFailureCode(err))
				turn.fail()
				for range turn.chunks {
				}
				return
			}
		}
		if turnCtx.Err() != nil {
			_ = rec.Close() // the turn gave up waiting for this recognition while it was still opening
		}
	}()
}

// feedLiveTurnLocked adds a chunk to the turn's recognition, starting it at the
// first voiced chunk. Called with p.mu held, after the chunk joined p.buffer.
func (p *Provider) feedLiveTurnLocked(ctx context.Context, chunk []byte, voiced bool) {
	if p.recognizer == nil || p.speaker.WantsDiarization() || p.speaker.Enabled {
		return
	}
	if p.liveTurn == nil {
		if !voiced {
			return
		}
		keep := recognitionPreRollMs * 32 // 16 kHz mono signed 16-bit: 32 bytes per millisecond
		start := len(p.buffer) - len(chunk) - keep
		if start < 0 {
			start = 0
		}
		p.startLiveTurnLocked(ctx, append([]byte(nil), p.buffer[start:]...))
		return
	}
	if voiced {
		p.liveTurn.mu.Lock()
		p.liveTurn.endHeard = false
		p.liveTurn.mu.Unlock()
	}
	select {
	case p.liveTurn.chunks <- append([]byte(nil), chunk...):
	default:
		p.liveTurn.fail() // the recognition cannot keep up; the turn falls back to batch recognition
	}
}

// liveTurnEndedLocked reports whether the recogniser heard the speaker stop and
// the silence since the last voiced chunk is long enough to commit the turn.
func (p *Provider) liveTurnEndedLocked(silentFor int64) bool {
	turn := p.liveTurn
	if turn == nil || silentFor < earlyCommitSilenceMs {
		return false
	}
	turn.mu.Lock()
	defer turn.mu.Unlock()
	return turn.endHeard && !turn.failed
}

// takeLiveTurnLocked hands the turn's recognition to the caller and closes its
// queue. Called with p.mu held.
func (p *Provider) takeLiveTurnLocked() *liveTurn {
	turn := p.liveTurn
	p.liveTurn = nil
	if turn != nil {
		close(turn.chunks)
	}
	return turn
}

// recognized finishes the turn's streaming recognition and returns its text. ok
// is false when the batch recogniser must take over: the stream failed, did not
// finish in time, or recognised nothing.
func (t *liveTurn) recognized(ctx context.Context) (text string, ok bool) {
	defer t.cancel()
	waitCtx, cancel := context.WithTimeout(ctx, recognitionFinishTimeout)
	defer cancel()
	select {
	case <-t.worker:
	case <-waitCtx.Done():
		return "", false
	}
	t.mu.Lock()
	rec, failed := t.rec, t.failed
	t.mu.Unlock()
	if rec == nil || failed {
		if rec != nil {
			_ = rec.Close()
		}
		return "", false
	}
	text, err := rec.Finish(waitCtx)
	_ = rec.Close()
	text = strings.TrimSpace(text)
	if err != nil || text == "" {
		return "", false
	}
	return text, true
}

// abandonLiveTurn drops a turn's recognition without using its result.
func (t *liveTurn) abandon() {
	t.cancel()
	go func() {
		<-t.worker
		t.mu.Lock()
		rec := t.rec
		t.mu.Unlock()
		if rec != nil {
			_ = rec.Close()
		}
	}()
}
