package wakeword

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
)

// TrainingCapture buffers the rolling PCM stream around wake-word
// detections and writes each detection's audio to disk as a WAV file
// plus a JSON sidecar describing the trigger. Foundation of v0.37.4's
// optional training-data pipeline — every boolean defaults to OFF so
// the structure only acts on host opt-in.
//
// All audio is 16 kHz mono S16 PCM matching the rest of the wake-word
// kernel (SampleRate / Channels / BytesPerSample). The pre-roll and
// post-roll knobs are expressed in milliseconds and converted to
// samples at construction time.
//
// Lifecycle (sidecar usage):
//
//	c, err := wakeword.NewTrainingCapture(wakeword.TrainingCaptureConfig{
//	    Enabled:    cfg.LocalCaptureEnabled,
//	    Dir:        cfg.LocalCaptureDir,
//	    PreRollMs:  cfg.PreRollMs,
//	    PostRollMs: cfg.PostRollMs,
//	    Backend:    "livekit_openwakeword",
//	    OnWrite:    func(rec TrainingRecord) { emit(...) },
//	})
//	defer c.Close()
//	// in PCM handler:
//	c.Ingest(pcm)
//	// on detection:
//	c.Trigger(detectionEvent)
//
// Capture is goroutine-safe. Ingest and Trigger may be called
// concurrently though typical sidecars use one PCM thread for both.
type TrainingCapture struct {
	enabled    bool
	dir        string
	backend    string
	preRoll    int // samples
	postRoll   int // samples
	onWrite    TrainingRecordHandler
	timeSource func() time.Time

	mu       sync.Mutex
	ring     []int16
	ringHead int
	ringFull bool
	pending  []*pendingCapture
}

// TrainingCaptureConfig is the constructor input.
type TrainingCaptureConfig struct {
	// Enabled is the master switch. When false NewTrainingCapture
	// still returns a non-nil capture but Ingest/Trigger become
	// no-ops so the sidecar can wire the same call sites
	// regardless of user opt-in state.
	Enabled bool

	// Dir is the filesystem root where captures land. Must already
	// exist (the constructor does NOT create it). Each capture
	// writes two files: <prefix>.wav and <prefix>.json.
	Dir string

	// PreRollMs and PostRollMs determine how much audio gets
	// captured before and after a trigger. Internally converted to
	// samples at the canonical SampleRate.
	PreRollMs  int
	PostRollMs int

	// Backend identifies the detector backend that produced the
	// trigger ("livekit_openwakeword" / "sherpa_kws" /
	// "stt_phrase"). Echoed into the JSON sidecar so labelers
	// know which detector to test against.
	Backend string

	// OnWrite is an optional callback invoked once per completed
	// capture flush. Nil is allowed.
	OnWrite TrainingRecordHandler

	// TimeSource overrides time.Now for tests.
	TimeSource func() time.Time
}

// TrainingRecord is the metadata describing one written capture. The
// JSON sidecar uses the same field names (lower_snake_case) so the
// Go struct is the canonical schema.
type TrainingRecord struct {
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

// TrainingRecordHandler is the OnWrite callback shape.
type TrainingRecordHandler func(TrainingRecord)

type pendingCapture struct {
	id         string
	phraseID   string
	phrase     string
	score      float32
	capturedAt time.Time
	audio      []int16
	remaining  int // samples still owed for post-roll
}

// NewTrainingCapture validates the config and returns a ready-to-use
// capture. When Enabled is false the returned capture is inert.
func NewTrainingCapture(cfg TrainingCaptureConfig) (*TrainingCapture, error) {
	c := &TrainingCapture{
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
		return nil, errors.New("wakeword: TrainingCapture pre_roll_ms must be > 0 when enabled")
	}
	if c.postRoll < 0 {
		return nil, errors.New("wakeword: TrainingCapture post_roll_ms must be >= 0 when enabled")
	}
	if c.dir == "" {
		return nil, errors.New("wakeword: TrainingCapture dir must be set when enabled")
	}
	if info, err := os.Stat(c.dir); err != nil {
		return nil, fmt.Errorf("wakeword: TrainingCapture dir %q: %w", c.dir, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("wakeword: TrainingCapture dir %q is not a directory", c.dir)
	}
	c.ring = make([]int16, c.preRoll)
	return c, nil
}

// Enabled reports whether the capture actively writes to disk.
func (c *TrainingCapture) Enabled() bool { return c != nil && c.enabled }

// Ingest pushes a fresh PCM buffer through the ring + any pending
// post-roll collectors.
func (c *TrainingCapture) Ingest(pcm []byte) {
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

// Trigger reacts to a wake-word DetectionEvent by snapshotting the
// current pre-roll ring and starting a post-roll collector. When
// post_roll is zero the capture flushes immediately.
func (c *TrainingCapture) Trigger(ev DetectionEvent) {
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
	cap := &pendingCapture{
		id:         newCaptureID(at),
		phraseID:   normalisePhraseID(firstNonEmpty(ev.Keyword, ev.Phrase)),
		phrase:     ev.Phrase,
		score:      ev.Probability,
		capturedAt: at,
		audio:      make([]int16, 0, len(preRoll)+c.postRoll),
		remaining:  c.postRoll,
	}
	cap.audio = append(cap.audio, preRoll...)
	if cap.remaining <= 0 {
		c.flushLocked(cap)
		return
	}
	c.pending = append(c.pending, cap)
}

// Close flushes any still-pending captures with whatever audio they
// already have so training clips are not silently lost on shutdown.
// Close is idempotent.
func (c *TrainingCapture) Close() error {
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

func (c *TrainingCapture) snapshotRingLocked() []int16 {
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

func (c *TrainingCapture) flushLocked(p *pendingCapture) {
	wavBytes := encodeWAV(p.audio)
	prefix := filepath.Join(c.dir, captureFilename(p))
	wavPath := prefix + ".wav"
	if err := os.WriteFile(wavPath, wavBytes, 0o600); err != nil {
		if c.onWrite != nil {
			c.onWrite(TrainingRecord{})
		}
		return
	}
	rec := TrainingRecord{
		ID:         p.id,
		PhraseID:   p.phraseID,
		Phrase:     p.phrase,
		Backend:    c.backend,
		Score:      p.score,
		CapturedAt: p.capturedAt,
		PreRollMs:  samplesToMs(c.preRoll),
		PostRollMs: samplesToMs(c.postRoll),
		SampleRate: SampleRate,
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

// randomHex8 returns 8 hex chars of crypto-quality randomness. The
// sidecar tolerates a brief delay here; this only runs per
// detection trigger.
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

// encodeWAV writes a canonical RIFF/WAVE header followed by S16LE
// PCM. Header is 44 bytes for a PCM/mono/16-bit/16kHz file.
func encodeWAV(samples []int16) []byte {
	const (
		bitsPerSample = 16
		channels      = uint16(Channels)
		blockAlign    = channels * bitsPerSample / 8
	)
	byteRate := uint32(SampleRate) * uint32(blockAlign)
	dataSize := uint32(len(samples) * 2)
	out := make([]byte, 0, 44+int(dataSize))
	out = append(out, []byte("RIFF")...)
	out = appendLE32(out, 36+dataSize)
	out = append(out, []byte("WAVE")...)
	out = append(out, []byte("fmt ")...)
	out = appendLE32(out, 16)
	out = appendLE16(out, 1)
	out = appendLE16(out, channels)
	out = appendLE32(out, uint32(SampleRate))
	out = appendLE32(out, byteRate)
	out = appendLE16(out, blockAlign)
	out = appendLE16(out, bitsPerSample)
	out = append(out, []byte("data")...)
	out = appendLE32(out, dataSize)
	for _, s := range samples {
		out = appendLE16(out, uint16(s)) //nolint:gosec // S16 -> bit-identical uint16
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
	n := len(pcm) / BytesPerSample
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(binary.LittleEndian.Uint16(pcm[i*BytesPerSample : (i+1)*BytesPerSample])) //nolint:gosec // S16LE: bit-identical reinterpret
	}
	return out
}

func msToSamples(ms int) int {
	if ms <= 0 {
		return 0
	}
	return (ms*SampleRate + 999) / 1000
}

func samplesToMs(samples int) int {
	if samples <= 0 {
		return 0
	}
	return (samples * 1000) / SampleRate
}
