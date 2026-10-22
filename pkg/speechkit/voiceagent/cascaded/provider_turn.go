package cascaded

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/internal/logutil"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

// -- internals --------------------------------------------------------

func (p *Provider) maybeTriggerLocked() {
	bufMs := PCMDurationMs(p.buffer)
	if bufMs < int64(p.cfg.MinTurnMs) {
		return
	}
	now := time.Now()
	silentFor := now.Sub(p.lastVoiceAt).Milliseconds()
	if silentFor >= int64(p.cfg.SilenceTurnMs) || bufMs >= int64(p.cfg.MaxTurnMs) || p.liveTurnEndedLocked(silentFor) {
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
	if ctx.Err() != nil {
		return
	}
	p.mu.Lock()
	if len(p.buffer) == 0 {
		p.mu.Unlock()
		return
	}
	pcm := p.buffer
	p.buffer = nil
	p.lastVoiceAt = time.Time{}
	live := p.takeLiveTurnLocked()
	p.mu.Unlock()

	duration := float64(PCMDurationMs(pcm)) / 1000.0
	emit := func(m *Message) {
		select {
		case p.messages <- m:
		case <-p.closedCh:
		case <-ctx.Done():
		}
	}

	sttStarted := time.Now()
	var sttResult *stt.Result
	var err error
	streamed := false
	if live != nil {
		// The recogniser has heard the turn while it was spoken: its transcript is ready or
		// nearly so. Any turn it cannot serve is recognised from the buffered audio below.
		if text, ok := live.recognized(ctx); ok {
			sttResult = &stt.Result{Text: text}
			streamed = true
		}
	}
	if sttResult == nil {
		sttResult, err = p.stt.Route(ctx, pcm, duration, stt.TranscribeOpts{Language: p.locale, Speaker: p.speaker})
	}
	p.noteSTT(time.Since(sttStarted), streamed)
	if err != nil {
		if ctx.Err() == nil {
			p.emitError("stt_failed")
		}
		return
	}
	if ctx.Err() != nil {
		return
	}
	if sttResult == nil || strings.TrimSpace(sttResult.Text) == "" {
		// Silent or empty turn - drop quietly.
		return
	}
	emit(inputTranscriptMessage(sttResult))

	if err := p.runTurn(ctx, sttResult.Text, true); err != nil && ctx.Err() == nil {
		p.emitError(turnFailureCode(err))
	}
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

func (p *Provider) runTurn(ctx context.Context, userText string, skipInputTranscript bool) (turnErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	timing := newTurnTiming()
	if sttMs, streamed, ok := p.takeSTT(); ok {
		timing.set("stt_ms", sttMs)
		if streamed {
			timing.set("stt_streamed", 1) // the live recogniser heard the turn while it was spoken
		} else {
			timing.set("stt_streamed", 0)
		}
	}
	defer func() {
		if turnErr != nil || ctx.Err() != nil {
			return
		}
		select {
		case p.messages <- &Message{Done: true, Metadata: timing.snapshot(p.agent)}:
		case <-p.closedCh:
		case <-ctx.Done():
		}
	}()
	if !skipInputTranscript {
		select {
		case p.messages <- &Message{InputTranscript: userText, InputTranscriptDone: true}:
		case <-p.closedCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	locale, voice, systemPrompt := p.currentInstructionSnapshot()
	historyBlurb := p.renderHistorySnapshot()
	agentInput := AgentInput{
		Utterance:         userText,
		Locale:            locale,
		Selection:         "",
		LastTranscription: historyBlurb,
		SystemPrompt:      systemPrompt,
	}
	if streamer, ok := p.agent.(StreamingAgent); ok && p.tts != nil {
		return p.runStreamedTurn(ctx, streamer, userText, agentInput, locale, voice, timing)
	}
	out, err := p.agent.Run(ctx, agentInput)
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}
	timing.mark("agent_ms")
	if err := ctx.Err(); err != nil {
		return err
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
	case <-ctx.Done():
		return ctx.Err()
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttsResult != nil && len(ttsResult.Audio) > 0 {
		timing.mark("first_audio_ms")
		for _, frag := range ChunkAudio(ttsResult.Audio, 8192) {
			select {
			case p.messages <- &Message{Audio: frag}:
			case <-p.closedCh:
				return nil
			case <-ctx.Done():
				return ctx.Err()
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
	if p.sessionCtx != nil && p.sessionCtx.Err() != nil {
		return
	}
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

// emitError reports a failed turn as an error message, never as transcript
// text: "[turn_failed] ..." used to reach clients as the agent's reply.
func (p *Provider) emitError(code string) {
	code = SafeFailureCode(code)
	logutil.Resolve(p.logger).Warn("cascaded: emit error", "code", code)
	select {
	case p.messages <- &Message{ErrorCode: code, ErrorMessage: FailureMessage(code)}:
	case <-p.closedCh:
	}
}

// turnFailureCode is the agent's typed reason for a failed turn, or
// "turn_failed" when it names none.
func turnFailureCode(err error) string {
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		return "turn_timeout"
	}
	var coded CodedError
	if errors.As(err, &coded) {
		if code := strings.TrimSpace(coded.Code()); code != "" {
			return SafeFailureCode(code)
		}
	}
	return "turn_failed"
}
