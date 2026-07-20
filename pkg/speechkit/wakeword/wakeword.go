// Package wakeword exposes embeddable SpeechKit wake-word contracts.
package wakeword

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	SampleRate     = 16000
	BytesPerSample = 2
	Channels       = 1
	FrameSamples   = 1280
	FrameBytes     = FrameSamples * BytesPerSample
	FrameDur       = 80 * time.Millisecond
	SourceWakeword = "wakeword"
)

// Config controls wake-word pipeline behavior.
type Config struct {
	Enabled              bool
	Phrase               string
	ModelDir             string
	KeywordsFile         string
	Keywords             []string
	DefaultMode          string
	Threshold            float32
	MinConsecutiveFrames int
	Cooldown             time.Duration
	AutoEnd              AutoEndConfig
}

// DetectionEvent is emitted when a keyword fires.
type DetectionEvent struct {
	Phrase  string
	Keyword string
	Mode    string
	// Probability is a confidence lower bound for the detection. The
	// sherpa-onnx KWS binding exposes no exact per-detection score, so the
	// live pipeline reports the effective keyword threshold the detection
	// cleared (i.e. "at least this confident") rather than a fabricated 1.0.
	Probability float32
	At          time.Time
}

// Sink receives DetectionEvents from a wake-word pipeline.
type Sink interface {
	Emit(DetectionEvent)
}

// SinkFunc adapts a plain function to Sink.
type SinkFunc func(DetectionEvent)

// Emit calls f.
func (f SinkFunc) Emit(ev DetectionEvent) { f(ev) }

// HotkeyEvent is the minimal event shape needed to bridge wake-word triggers
// into a host's mode activation path.
type HotkeyEvent struct {
	KeyDown bool
	Binding string
	Source  string
}

// HotkeySink consumes synthetic key events.
type HotkeySink interface {
	Submit(HotkeyEvent)
}

// HotkeySinkFunc adapts a plain function to HotkeySink.
type HotkeySinkFunc func(HotkeyEvent)

// Submit calls f.
func (f HotkeySinkFunc) Submit(ev HotkeyEvent) { f(ev) }

// Dispatcher converts DetectionEvents into synthetic hotkey events.
type Dispatcher struct {
	sink             HotkeySink
	logger           *slog.Logger
	syntheticRelease bool
	releaseAfter     time.Duration

	mu       sync.Mutex
	closed   bool
	inFlight sync.WaitGroup
}

// DispatcherOptions tweaks Dispatcher behavior.
type DispatcherOptions struct {
	SyntheticRelease bool
	ReleaseAfter     time.Duration
	Logger           *slog.Logger
}

// NewDispatcher constructs a Dispatcher with the given sink and options.
func NewDispatcher(sink HotkeySink, opts DispatcherOptions) *Dispatcher {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.ReleaseAfter <= 0 {
		opts.ReleaseAfter = 150 * time.Millisecond
	}
	return &Dispatcher{
		sink:             sink,
		logger:           opts.Logger,
		syntheticRelease: opts.SyntheticRelease,
		releaseAfter:     opts.ReleaseAfter,
	}
}

// Emit implements Sink. It is non-blocking for the caller.
func (d *Dispatcher) Emit(ev DetectionEvent) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	if ev.Mode == "" {
		d.mu.Unlock()
		d.logger.Warn("wake event without mode; ignoring", "phrase", ev.Phrase)
		return
	}
	d.inFlight.Add(1)
	d.mu.Unlock()

	d.logger.Info("wakeword fired",
		"phrase", ev.Phrase,
		"mode", ev.Mode,
		"probability", ev.Probability)

	go d.dispatch(ev)
}

// Close blocks further events and waits for in-flight dispatches.
func (d *Dispatcher) Close(ctx context.Context) error {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	done := make(chan struct{})
	go func() {
		d.inFlight.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Dispatcher) dispatch(ev DetectionEvent) {
	defer d.inFlight.Done()
	d.sink.Submit(HotkeyEvent{KeyDown: true, Binding: ev.Mode, Source: SourceWakeword})
	if !d.syntheticRelease {
		return
	}
	time.Sleep(d.releaseAfter)
	d.sink.Submit(HotkeyEvent{KeyDown: false, Binding: ev.Mode, Source: SourceWakeword})
}

// AutoEndConfig controls when a wake-triggered session automatically ends.
type AutoEndConfig struct {
	SilenceCutoff time.Duration
	ExitPhrases   []string
}

// DefaultAutoEndConfig returns SpeechKit's framework baseline.
func DefaultAutoEndConfig() AutoEndConfig {
	return AutoEndConfig{
		SilenceCutoff: 10 * time.Second,
		ExitPhrases: []string{
			"danke", "tschuess", "tschüss", "ende", "stop",
			"thanks", "thank you", "bye", "goodbye",
		},
	}
}

// EndReason is the result emitted by AutoEndPolicy.
type EndReason string

const (
	EndReasonSilence    EndReason = "silence"
	EndReasonExitPhrase EndReason = "exit_phrase"
)

// AutoEndPolicy watches activity/transcripts and fires an end signal once.
type AutoEndPolicy struct {
	cfg    AutoEndConfig
	logger *slog.Logger

	mu      sync.Mutex
	started bool
	closed  bool
	fired   bool
	timer   *time.Timer
	endCh   chan EndReason
}

// NewAutoEndPolicy constructs a policy from config.
func NewAutoEndPolicy(cfg AutoEndConfig, logger *slog.Logger) *AutoEndPolicy {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.SilenceCutoff <= 0 && len(cfg.ExitPhrases) == 0 {
		cfg = DefaultAutoEndConfig()
	}
	return &AutoEndPolicy{
		cfg:    cfg,
		logger: logger,
		endCh:  make(chan EndReason, 1),
	}
}

// Config returns a copy of the policy config.
func (p *AutoEndPolicy) Config() AutoEndConfig {
	cfg := p.cfg
	if len(p.cfg.ExitPhrases) > 0 {
		cfg.ExitPhrases = append([]string(nil), p.cfg.ExitPhrases...)
	}
	return cfg
}

// Start arms the silence timer.
func (p *AutoEndPolicy) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started || p.closed {
		return
	}
	p.started = true
	p.resetSilenceTimerLocked()
}

// NotifyActivity resets the silence timer.
func (p *AutoEndPolicy) NotifyActivity() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started || p.closed || p.fired {
		return
	}
	p.resetSilenceTimerLocked()
}

// NotifyTranscript checks text against configured exit phrases.
func (p *AutoEndPolicy) NotifyTranscript(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started || p.closed || p.fired {
		return
	}
	p.resetSilenceTimerLocked()
	lowered := strings.ToLower(text)
	for _, phrase := range p.cfg.ExitPhrases {
		phrase = strings.ToLower(strings.TrimSpace(phrase))
		if phrase == "" {
			continue
		}
		if strings.Contains(lowered, phrase) {
			p.logger.Info("wakeword: exit phrase matched, ending session",
				"phrase", phrase, "transcript_snippet", text)
			p.fireLocked(EndReasonExitPhrase)
			return
		}
	}
}

// EndSignal fires when the policy decides the session should end.
func (p *AutoEndPolicy) EndSignal() <-chan EndReason {
	return p.endCh
}

// Close stops timers and closes EndSignal if it has not fired.
func (p *AutoEndPolicy) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	if !p.fired {
		close(p.endCh)
	}
}

func (p *AutoEndPolicy) resetSilenceTimerLocked() {
	if p.cfg.SilenceCutoff <= 0 {
		return
	}
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(p.cfg.SilenceCutoff, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.closed || p.fired || !p.started {
			return
		}
		p.logger.Info("wakeword: silence cutoff reached, ending session",
			"silence_cutoff", p.cfg.SilenceCutoff)
		p.fireLocked(EndReasonSilence)
	})
}

func (p *AutoEndPolicy) fireLocked(reason EndReason) {
	if p.fired || p.closed {
		return
	}
	p.fired = true
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	select {
	case p.endCh <- reason:
		close(p.endCh)
	default:
		p.logger.Warn("wakeword: AutoEndPolicy end channel full", "reason", reason)
	}
}

// PhraseCatalogEntry describes a curated wake phrase.
type PhraseCatalogEntry struct {
	ID           string
	DisplayName  string
	KeywordLabel string
	Notes        string
}

// DefaultCatalog returns SpeechKit's curated wake-phrase list.
func DefaultCatalog() []PhraseCatalogEntry {
	return []PhraseCatalogEntry{
		{
			ID:           "hey_quby",
			DisplayName:  "Hey Quby (Cubi / Kubi)",
			KeywordLabel: "hey_quby",
			Notes:        "SpeechKit brand default. Distinct K/B consonants, varied vowels.",
		},
		{
			ID:           "hey_computer",
			DisplayName:  "Hey Computer",
			KeywordLabel: "hey_computer",
			Notes:        "Star Trek classic. 4 syllables, very distinct phonemes.",
		},
		{
			ID:           "hey_jarvis",
			DisplayName:  "Hey Jarvis",
			KeywordLabel: "hey_jarvis",
			Notes:        "Marvel-popular. Strong J/RV/S consonants.",
		},
		{
			ID:           "hey_mira",
			DisplayName:  "Hey Mira",
			KeywordLabel: "hey_mira",
			Notes:        "Short brand-style alternative; equally natural in DE+EN.",
		},
		{
			ID:           "hey_kombify",
			DisplayName:  "Hey Kombify",
			KeywordLabel: "hey_kombify",
			Notes:        "Organisation brand. 4 syllables, three distinct consonant onsets (K/B/F).",
		},
	}
}

// LookupPhrase returns the catalog entry matching id.
func LookupPhrase(id string) *PhraseCatalogEntry {
	key := strings.ToLower(strings.TrimSpace(id))
	if key == "" {
		return nil
	}
	for _, entry := range DefaultCatalog() {
		if entry.ID == key {
			out := entry
			return &out
		}
	}
	return nil
}

// FrameDuration returns the fixed PCM frame duration.
func FrameDuration() time.Duration { return FrameDur }
