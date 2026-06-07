package cascaded

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/firebase/genkit/go/core"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/tts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// Provider is a turn-based STT -> LLM -> TTS voice agent. It implements
// the small contract documented on this type's methods; adapters in
// internal/server/voiceagent and internal/voiceagent wrap it into the
// richer LiveProvider interface their callers expect.
type Provider struct {
	stt             STT
	agent           Agent
	tts             TTS
	speakerStreamer speaker.StreamingProvider
	cfg             Config

	// Per-session live config from Connect()
	locale       string
	voice        string
	systemPrompt string
	refinement   string
	speaker      speaker.Options

	// Turn state
	mu          sync.Mutex
	buffer      []byte
	lastVoiceAt time.Time
	history     []conversationTurn

	// Processing channels
	messages  chan *Message
	triggers  chan struct{}
	closeOnce sync.Once
	closedCh  chan struct{}

	speakerStreamMu     sync.Mutex
	speakerStream       speaker.SpeakerStream
	speakerStreamCancel context.CancelFunc
}

// STT is the STT surface the provider uses. In production this is the
// same *internal/router.Router that serves /v1/dictation/transcribe.
type STT interface {
	Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error)
}

// Agent is the LLM surface the provider uses. In production this is the
// Genkit agent flow defined by flows.DefineAgentFlow.
type Agent interface {
	Run(ctx context.Context, input flows.AgentInput) (flows.AgentOutput, error)
}

// TTS is the TTS surface the provider uses. Optional; nil drops audio
// frames and keeps OutputTranscript-only emission.
type TTS interface {
	Synthesize(ctx context.Context, text string, opts tts.SynthesizeOpts) (*tts.Result, error)
}

// Deps bundles everything the bootstrap hands to the provider.
type Deps struct {
	STT             STT
	Agent           Agent
	TTS             TTS
	SpeakerStreamer speaker.StreamingProvider
	Config          Config
}

// NewProvider constructs a provider without starting background work.
// Connect() initializes the goroutine that performs turn processing.
func NewProvider(deps Deps) *Provider {
	cfg := deps.Config.WithDefaults()
	return &Provider{
		stt:             deps.STT,
		agent:           deps.Agent,
		tts:             deps.TTS,
		speakerStreamer: deps.SpeakerStreamer,
		cfg:             cfg,
		messages:        make(chan *Message, 16),
		triggers:        make(chan struct{}, 4),
		closedCh:        make(chan struct{}),
	}
}

// Connect validates that the required dependencies are satisfied and
// starts the processor loop. Unlike Gemini Live, no external handshake
// happens.
func (p *Provider) Connect(ctx context.Context, cfg SessionConfig) error {
	if p.stt == nil {
		return errors.New("cascaded: STT router not configured")
	}
	if p.agent == nil {
		return errors.New("cascaded: Agent flow not configured (no LLM models available)")
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
		return nil, errors.New("cascaded: provider closed")
	case msg, ok := <-p.messages:
		if !ok {
			return nil, errors.New("cascaded: message channel closed")
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

// -- internals --------------------------------------------------------

func (p *Provider) maybeTriggerLocked() {
	bufMs := PCMDurationMs(p.buffer)
	if bufMs < int64(p.cfg.MinTurnMs) {
		return
	}
	now := time.Now()
	silentFor := now.Sub(p.lastVoiceAt).Milliseconds()
	if silentFor >= int64(p.cfg.SilenceTurnMs) || bufMs >= int64(p.cfg.MaxTurnMs) {
		p.fire()
	}
}

func (p *Provider) fire() {
	select {
	case p.triggers <- struct{}{}:
	default:
	}
}

func (p *Provider) processorLoop(parent context.Context) {
	for {
		select {
		case <-parent.Done():
			return
		case <-p.closedCh:
			return
		case <-p.triggers:
			p.processOneTurn(parent)
		}
	}
}

func (p *Provider) processOneTurn(ctx context.Context) {
	p.mu.Lock()
	if len(p.buffer) == 0 {
		p.mu.Unlock()
		return
	}
	pcm := p.buffer
	p.buffer = nil
	p.lastVoiceAt = time.Time{}
	p.mu.Unlock()

	duration := float64(PCMDurationMs(pcm)) / 1000.0
	emit := func(m *Message) {
		select {
		case p.messages <- m:
		case <-p.closedCh:
		}
	}

	sttResult, err := p.stt.Route(ctx, pcm, duration, stt.TranscribeOpts{Language: p.locale, Speaker: p.speaker})
	if err != nil {
		p.emitError("stt_failed", err.Error())
		return
	}
	if sttResult == nil || strings.TrimSpace(sttResult.Text) == "" {
		// Silent or empty turn - drop quietly.
		return
	}
	emit(inputTranscriptMessage(sttResult))

	if err := p.runTurn(ctx, sttResult.Text, true); err != nil {
		p.emitError("turn_failed", err.Error())
	}
}

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

func inputTranscriptMessage(result *stt.Result) *Message {
	if result == nil {
		return &Message{InputTranscriptDone: true}
	}
	msg := &Message{
		InputTranscript:     strings.TrimSpace(result.Text),
		InputTranscriptDone: true,
	}
	if result.Speakers == nil || len(result.Speakers.Segments) == 0 {
		return msg
	}
	segment := result.Speakers.Segments[0]
	msg.InputSpeakerLabel = segment.SpeakerLabel
	msg.InputPersonID = segment.PersonID
	msg.InputDisplayName = segment.DisplayName
	msg.InputSpeakerConfidence = segment.SpeakerConfidence
	return msg
}

func inputTranscriptMessageFromSpeakerFrame(frame *speaker.SpeakerFrame) *Message {
	if frame == nil || strings.TrimSpace(frame.Text) == "" {
		return nil
	}
	msg := &Message{
		InputTranscript:     strings.TrimSpace(frame.Text),
		InputTranscriptDone: frame.IsFinal,
	}
	if frame.Segment != nil {
		msg.InputSpeakerLabel = frame.Segment.SpeakerLabel
		msg.InputPersonID = frame.Segment.PersonID
		msg.InputDisplayName = frame.Segment.DisplayName
		msg.InputSpeakerConfidence = frame.Segment.SpeakerConfidence
		return msg
	}
	if len(frame.Speakers) > 0 {
		msg.InputSpeakerLabel = frame.Speakers[0].Label
		msg.InputPersonID = frame.Speakers[0].PersonID
		msg.InputDisplayName = frame.Speakers[0].DisplayName
		msg.InputSpeakerConfidence = frame.Speakers[0].Confidence
	}
	return msg
}

func (p *Provider) runTurn(ctx context.Context, userText string, skipInputTranscript bool) error {
	if !skipInputTranscript {
		select {
		case p.messages <- &Message{InputTranscript: userText, InputTranscriptDone: true}:
		case <-p.closedCh:
			return nil
		}
	}

	locale, voice, systemPrompt := p.currentInstructionSnapshot()
	historyBlurb := p.renderHistorySnapshot()
	agentInput := flows.AgentInput{
		Utterance:         userText,
		Locale:            locale,
		Selection:         "",
		LastTranscription: historyBlurb,
		SystemPrompt:      systemPrompt,
	}
	out, err := p.agent.Run(ctx, agentInput)
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}
	responseText := strings.TrimSpace(out.Text)
	if responseText == "" {
		return nil
	}
	p.appendHistory(userText, responseText)

	select {
	case p.messages <- &Message{OutputTranscript: responseText, OutputTranscriptDone: true}:
	case <-p.closedCh:
		return nil
	}

	if p.tts == nil {
		return nil
	}
	ttsResult, err := p.tts.Synthesize(ctx, responseText, tts.SynthesizeOpts{
		Locale: locale,
		Voice:  voice,
		Speed:  p.cfg.TTSSpeed,
		Format: p.cfg.TTSFormat,
	})
	if err != nil {
		return fmt.Errorf("tts: %w", err)
	}
	if ttsResult != nil && len(ttsResult.Audio) > 0 {
		for _, frag := range ChunkAudio(ttsResult.Audio, 8192) {
			select {
			case p.messages <- &Message{Audio: frag}:
			case <-p.closedCh:
				return nil
			}
		}
	}
	return nil
}

func (p *Provider) currentInstructionSnapshot() (locale, voice, systemPrompt string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.locale, p.voice, renderSystemPrompt(p.systemPrompt, p.refinement)
}

func (p *Provider) appendHistory(user, assistant string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.history = append(p.history, conversationTurn{User: user, Assistant: assistant})
	if len(p.history) > p.cfg.HistoryTurns {
		p.history = p.history[len(p.history)-p.cfg.HistoryTurns:]
	}
}

func (p *Provider) renderHistorySnapshot() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return renderHistory(p.history)
}

func (p *Provider) emitError(code, message string) {
	slog.Warn("cascaded: emit error", "code", code, "err", message)
	select {
	case p.messages <- &Message{OutputTranscript: "[" + code + "] " + message, OutputTranscriptDone: true}:
	case <-p.closedCh:
	}
}

// -- agent flow adapter -----------------------------------------------

type agentFlowAdapter struct {
	Flow *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}]
}

// NewAgentFlowAdapter wraps a Genkit agent flow so it satisfies Agent.
// Returned as a separate named type to keep production wiring readable.
func NewAgentFlowAdapter(flow *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}]) Agent {
	return &agentFlowAdapter{Flow: flow}
}

func (a *agentFlowAdapter) Run(ctx context.Context, input flows.AgentInput) (flows.AgentOutput, error) {
	if a.Flow == nil {
		return flows.AgentOutput{}, errors.New("cascaded: agent flow is nil")
	}
	return a.Flow.Run(ctx, input)
}
