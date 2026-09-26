// Package training captures wake-word activations for model training. A
// Capture keeps a rolling pre-roll buffer of the wake-word PCM stream and, on
// each DetectionEvent, writes pre-roll plus post-roll audio to disk as a WAV
// file with a JSON sidecar (Record) describing the trigger. An opt-in
// Uploader ships those records to a SpeechKit server. Every switch defaults
// to off: nothing is recorded or sent unless the host enables it.
package training

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
)

// Capture buffers the rolling PCM stream around wake-word detections and
// writes each detection's audio to disk as a WAV file plus a JSON sidecar
// describing the trigger.
//
// All audio is 16 kHz mono S16 PCM matching the wake-word pipeline
// (wakeword.SampleRate / Channels / BytesPerSample). The pre-roll and
// post-roll knobs are expressed in milliseconds and converted to samples at
// construction time.
//
// Lifecycle (sidecar usage):
//
//	c, err := training.NewCapture(training.CaptureConfig{
//	    Enabled:    cfg.LocalCaptureEnabled,
//	    Dir:        cfg.LocalCaptureDir,
//	    PreRollMs:  cfg.PreRollMs,
//	    PostRollMs: cfg.PostRollMs,
//	    Backend:    "sherpa-onnx-zipformer-kws",
//	    OnWrite:    func(rec training.Record) { emit(...) },
//	})
//	defer c.Close()
//	// in the PCM handler:
//	c.Ingest(pcm)
//	// on detection:
//	c.Trigger(detectionEvent)
//
// Capture is goroutine-safe: Ingest and Trigger may be called concurrently,
// though typical sidecars use one PCM thread for both.
type Capture struct {
	enabled    bool
	dir        string
	backend    string
	preRoll    int // samples
	postRoll   int // samples
	onWrite    RecordHandler
	timeSource func() time.Time

	mu       sync.Mutex
	ring     []int16
	ringHead int
	ringFull bool
	pending  []*pendingCapture
}

// CaptureConfig is the constructor input of NewCapture.
type CaptureConfig struct {
	// Enabled is the master switch. When false NewCapture still returns a
	// non-nil capture but Ingest/Trigger become no-ops, so the sidecar can
	// wire the same call sites regardless of user opt-in state.
	Enabled bool

	// Dir is the filesystem root where captures land. Must already exist
	// (the constructor does NOT create it). Each capture writes two files:
	// <prefix>.wav and <prefix>.json.
	Dir string

	// PreRollMs and PostRollMs determine how much audio gets captured
	// before and after a trigger. Internally converted to samples at the
	// canonical wakeword.SampleRate.
	PreRollMs  int
	PostRollMs int

	// Backend identifies the detector backend that produced the trigger
	// (e.g. "sherpa-onnx-zipformer-kws" / "livekit-openwakeword-onnx" /
	// "stt_phrase"). Echoed into the JSON sidecar so labelers know which
	// detector to test against.
	Backend string

	// OnWrite is an optional callback invoked once per completed capture
	// flush. Nil is allowed.
	OnWrite RecordHandler

	// TimeSource overrides time.Now for tests.
	TimeSource func() time.Time
}

// Record is the metadata describing one written capture. The JSON sidecar
// uses the same field names (lower_snake_case), so this struct is the
// canonical schema.
type Record struct {
	ID         string    `json:"id"`
	PhraseID   string    `json:"phrase_id"`
	Phrase     string    `json:"phrase"`
	Backend    string    `json:"backend"`
	Score      float32   `json:"score"`
	CapturedAt time.Time `json:"captured_at"`
	PreRollMs  int       `json:"pre_roll_ms"`
	PostRollMs int       `json:"post_roll_ms"`
	SampleRate int       `json:"sample_rate"`
	AudioPath  string    `json:"audio_path"`
	AudioBytes int       `json:"audio_bytes"`
	Label      string    `json:"label,omitempty"`
	Uploaded   bool      `json:"uploaded"`
}

// RecordHandler is the OnWrite callback shape. A flush that failed before
// any disk write reports a zero Record (empty ID).
type RecordHandler func(Record)

type pendingCapture struct {
	id         string
	phraseID   string
	phrase     string
	score      float32
	capturedAt time.Time
	audio      []int16
	remaining  int // samples still owed for post-roll
}

// NewCapture validates cfg and returns a ready-to-use capture. When
// cfg.Enabled is false the returned capture is inert.
func NewCapture(cfg CaptureConfig) (*Capture, error) {
	c := &Capture{
		enabled:    cfg.Enabled,
		dir:        cfg.Dir,
		backend:    cfg.Backend,
		preRoll:    msToSamples(cfg.PreRollMs),
		postRoll:   msToSamples(cfg.PostRollMs),
		onWrite:    cfg.OnWrite,
		timeSource: cfg.TimeSource,
	}
	if c.timeSource == nil {
		c.timeSource = time.Now
	}
	if !c.enabled {
		return c, nil
	}
	if c.preRoll <= 0 {
		return nil, errors.New("training: capture pre_roll_ms must be > 0 when enabled")
	}
	if c.postRoll < 0 {
		return nil, errors.New("training: capture post_roll_ms must be >= 0 when enabled")
	}
	if c.dir == "" {
		return nil, errors.New("training: capture dir must be set when enabled")
	}
	if info, err := os.Stat(c.dir); err != nil {
		return nil, fmt.Errorf("training: capture dir %q: %w", c.dir, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("training: capture dir %q is not a directory", c.dir)
	}
	c.ring = make([]int16, c.preRoll)
	return c, nil
}

// Enabled reports whether the capture actively writes to disk.
func (c *Capture) Enabled() bool { return c != nil && c.enabled }

// Ingest pushes a fresh PCM buffer through the ring and any pending
// post-roll collectors.
func (c *Capture) Ingest(pcm []byte) {
	if !c.Enabled() || len(pcm) == 0 {
		return
	}
	samples := bytesToInt16(pcm)
	if len(samples) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, s := range samples {
		c.ring[c.ringHead] = s
		c.ringHead++
		if c.ringHead >= len(c.ring) {
			c.ringHead = 0
			c.ringFull = true
		}
	}

	if len(c.pending) == 0 {
		return
	}
	remaining := c.pending[:0]
	for _, p := range c.pending {
		take := len(samples)
		if take > p.remaining {
			take = p.remaining
		}
		if take > 0 {
			p.audio = append(p.audio, samples[:take]...)
			p.remaining -= take
		}
		if p.remaining <= 0 {
			c.flushLocked(p)
			continue
		}
		remaining = append(remaining, p)
	}
	c.pending = remaining
}

// Trigger reacts to a wake-word DetectionEvent by snapshotting the current
// pre-roll ring and starting a post-roll collector. When post-roll is zero
// the capture flushes immediately.
func (c *Capture) Trigger(ev wakeword.DetectionEvent) {
	if !c.Enabled() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	preRoll := c.snapshotRingLocked()
	at := ev.At
	if at.IsZero() {
		at = c.timeSource()
	}
	pc := &pendingCapture{
		id:         newCaptureID(at),
		phraseID:   normalisePhraseID(firstNonEmpty(ev.Keyword, ev.Phrase)),
		phrase:     ev.Phrase,
		score:      ev.Probability,
		capturedAt: at,
		audio:      make([]int16, 0, len(preRoll)+c.postRoll),
		remaining:  c.postRoll,
	}
	pc.audio = append(pc.audio, preRoll...)
	if pc.remaining <= 0 {
		c.flushLocked(pc)
		return
	}
	c.pending = append(c.pending, pc)
}

// Close flushes any still-pending captures with whatever audio they already
// have so training clips are not silently lost on shutdown. Close is
// idempotent.
func (c *Capture) Close() error {
	if !c.Enabled() {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.pending {
		if len(p.audio) > 0 {
			c.flushLocked(p)
		}
	}
	c.pending = nil
	return nil
}

func (c *Capture) snapshotRingLocked() []int16 {
	if !c.ringFull {
		out := make([]int16, c.ringHead)
		copy(out, c.ring[:c.ringHead])
		return out
	}
	out := make([]int16, len(c.ring))
	copy(out, c.ring[c.ringHead:])
	copy(out[len(c.ring)-c.ringHead:], c.ring[:c.ringHead])
	return out
}

func (c *Capture) flushLocked(p *pendingCapture) {
	wavBytes := encodeWAV(p.audio)
	prefix := filepath.Join(c.dir, captureFilename(p))
	wavPath := prefix + ".wav"
	if err := os.WriteFile(wavPath, wavBytes, 0o600); err != nil {
		if c.onWrite != nil {
			c.onWrite(Record{})
		}
		return
	}
	rec := Record{
		ID:         p.id,
		PhraseID:   p.phraseID,
		Phrase:     p.phrase,
		Backend:    c.backend,
		Score:      p.score,
		CapturedAt: p.capturedAt,
		PreRollMs:  samplesToMs(c.preRoll),
		PostRollMs: samplesToMs(c.postRoll),
		SampleRate: wakeword.SampleRate,
		AudioPath:  filepath.Base(wavPath),
		AudioBytes: len(wavBytes),
		Uploaded:   false,
	}
	if jsonBytes, err := json.MarshalIndent(rec, "", "  "); err == nil {
		_ = os.WriteFile(prefix+".json", jsonBytes, 0o600)
	}
	if c.onWrite != nil {
		c.onWrite(rec)
	}
}

func captureFilename(p *pendingCapture) string {
	ts := p.capturedAt.UTC().Format("2006-01-02T15-04-05.000Z")
	score := fmt.Sprintf("%.2f", p.score)
	phrase := p.phraseID
	if phrase == "" {
		phrase = "unknown"
	}
	return ts + "_" + phrase + "_" + score
}

func newCaptureID(now time.Time) string {
	return now.UTC().Format("20060102T150405.000Z") + "-" + randomHex8()
}

// randomHex8 returns 8 hex chars of crypto-quality randomness. It only runs
// once per detection trigger, so the sidecar tolerates the brief delay.
func randomHex8() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "00000000"
	}
	return fmt.Sprintf("%02x%02x%02x%02x", buf[0], buf[1], buf[2], buf[3])
}

func normalisePhraseID(raw string) string {
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32)
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			out = append(out, c)
		case c == ' ':
			out = append(out, '_')
		case c == '_', c == '-':
			out = append(out, c)
		}
	}
	return string(out)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// encodeWAV writes a canonical RIFF/WAVE header followed by S16LE PCM. The
// header is 44 bytes for a PCM/mono/16-bit/16 kHz file.
func encodeWAV(samples []int16) []byte {
	const (
		bitsPerSample = 16
		channels      = uint16(wakeword.Channels)
		blockAlign    = channels * bitsPerSample / 8
	)
	byteRate := uint32(wakeword.SampleRate) * uint32(blockAlign)
	dataSize := uint32(len(samples) * 2) // #nosec G115 -- local capture windows bound sample buffers well below WAV RIFF limits.
	out := make([]byte, 0, 44+int(dataSize))
	out = append(out, []byte("RIFF")...)
	out = appendLE32(out, 36+dataSize)
	out = append(out, []byte("WAVE")...)
	out = append(out, []byte("fmt ")...)
	out = appendLE32(out, 16)
	out = appendLE16(out, 1)
	out = appendLE16(out, channels)
	out = appendLE32(out, uint32(wakeword.SampleRate))
	out = appendLE32(out, byteRate)
	out = appendLE16(out, blockAlign)
	out = appendLE16(out, bitsPerSample)
	out = append(out, []byte("data")...)
	out = appendLE32(out, dataSize)
	for _, s := range samples {
		out = appendLE16(out, uint16(s)) // #nosec G115 -- S16 PCM is written as the same two-byte little-endian bit pattern.
	}
	return out
}

func appendLE32(dst []byte, v uint32) []byte {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	return append(dst, buf[:]...)
}

func appendLE16(dst []byte, v uint16) []byte {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], v)
	return append(dst, buf[:]...)
}

func bytesToInt16(pcm []byte) []int16 {
	n := len(pcm) / wakeword.BytesPerSample
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(binary.LittleEndian.Uint16(pcm[i*wakeword.BytesPerSample : (i+1)*wakeword.BytesPerSample])) // #nosec G115 -- S16LE PCM decoding reinterprets identical-width sample bits.
	}
	return out
}

func msToSamples(ms int) int {
	if ms <= 0 {
		return 0
	}
	return (ms*wakeword.SampleRate + 999) / 1000
}

func samplesToMs(samples int) int {
	if samples <= 0 {
		return 0
	}
	return (samples * 1000) / wakeword.SampleRate
}
