package cascaded

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

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
	agentInput := AgentInput{
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
