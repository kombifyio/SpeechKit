package cascaded

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

// StreamingAgent is an optional [Agent] extension. An agent that implements it
// hands over its answer while it is still being produced, and the provider
// speaks the answer sentence by sentence: the first audio no longer waits for
// the whole reply or for the synthesis of the whole reply. emit receives each
// answer delta in order; returning an error from emit aborts the turn. The
// returned [AgentOutput] carries the complete answer.
type StreamingAgent interface {
	Agent
	Stream(ctx context.Context, input AgentInput, emit func(delta string) error) (AgentOutput, error)
}

const (
	// speechLookahead bounds how many sentences are synthesized but not yet
	// played, so a long answer never piles up its whole audio in memory.
	speechLookahead = 3
	// minFirstClauseRunes lets the first spoken piece of a turn end at a clause
	// boundary (comma, semicolon, colon) once it is this long, which starts the
	// first audio earlier than waiting for a full sentence.
	minFirstClauseRunes = 28
	// minSegmentRunes keeps a very short sentence ("Ja.") joined to the next one.
	minSegmentRunes = 12
	// minFirstSegmentRunes is the shorter bound for the first piece of a turn ("Gerne!", "Hallo!", "Klar."): the first
	// audio should not wait for the second sentence only because the opening word is short.
	minFirstSegmentRunes = 5
	// maxSegmentRunes forces a split of a sentence that never ends.
	maxSegmentRunes = 220
)

// spokenSegment is one sentence on its way to the speaker: its text is already
// out as a transcript, its audio arrives on audio.
type spokenSegment struct {
	text  string
	audio chan segmentAudio
}

type segmentAudio struct {
	pcm []byte
	err error
}

// runStreamedTurn runs one agent turn with a streaming agent: transcript text
// leaves per sentence as it is complete, synthesis of the next sentences runs
// while the previous one plays, and audio frames follow in order.
func (p *Provider) runStreamedTurn(ctx context.Context, agent StreamingAgent, userText string, input AgentInput, locale, voice string, timing *turnTiming) error {
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	segments := make(chan *spokenSegment, speechLookahead+1)
	slots := make(chan struct{}, speechLookahead)
	played := make(chan error, 1)
	go func() { played <- p.playSegments(turnCtx, segments, slots, timing) }()

	var spoken strings.Builder
	splitter := &sentenceSplitter{}
	submit := func(text string) error {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		select {
		case slots <- struct{}{}:
		case <-turnCtx.Done():
			return turnCtx.Err()
		}
		timing.mark("agent_first_delta_ms")
		seg := &spokenSegment{text: text, audio: make(chan segmentAudio, 1)}
		go p.synthesizeSegment(turnCtx, seg, locale, voice)
		select {
		case segments <- seg:
		case <-turnCtx.Done():
			return turnCtx.Err()
		}
		if spoken.Len() > 0 {
			spoken.WriteByte(' ')
		}
		spoken.WriteString(text)
		// The text leaves with the sentence, ahead of its audio.
		select {
		case p.messages <- &Message{OutputTranscript: text, OutputTranscriptDone: true}:
		case <-p.closedCh:
			return ErrClosed
		case <-turnCtx.Done():
			return turnCtx.Err()
		}
		return nil
	}

	out, agentErr := agent.Stream(turnCtx, input, func(delta string) error {
		for _, sentence := range splitter.Push(delta) {
			if err := submit(sentence); err != nil {
				return err
			}
		}
		return nil
	})
	if agentErr == nil {
		agentErr = submit(splitter.Flush())
	}
	timing.mark("agent_ms")
	close(segments)
	if agentErr != nil {
		// What was already handed to the speaker still plays out.
		<-played
		return fmt.Errorf("agent: %w", agentErr)
	}
	playErr := <-played
	if err := ctx.Err(); err != nil {
		return err
	}
	answer := strings.TrimSpace(out.Text)
	if answer == "" {
		answer = spoken.String()
	}
	if answer != "" {
		p.appendHistory(userText, answer)
	}
	return playErr
}

func (p *Provider) synthesizeSegment(ctx context.Context, seg *spokenSegment, locale, voice string) {
	result, err := p.tts.Synthesize(ctx, seg.text, tts.SynthesizeOpts{
		Locale: locale,
		Voice:  voice,
		Speed:  p.cfg.TTSSpeed,
		Format: p.cfg.TTSFormat,
	})
	if err != nil {
		seg.audio <- segmentAudio{err: err}
		return
	}
	if result == nil {
		seg.audio <- segmentAudio{}
		return
	}
	seg.audio <- segmentAudio{pcm: result.Audio}
}

// playSegments emits the audio of each segment in order. It drains the queue
// after a failure so no submit is left blocked.
func (p *Provider) playSegments(ctx context.Context, segments <-chan *spokenSegment, slots <-chan struct{}, timing *turnTiming) error {
	var failure error
	for seg := range segments {
		if failure != nil {
			<-slots
			continue
		}
		var audio segmentAudio
		select {
		case audio = <-seg.audio:
		case <-ctx.Done():
			failure = ctx.Err()
		}
		<-slots
		if failure != nil {
			continue
		}
		if audio.err != nil {
			failure = fmt.Errorf("tts: %w", audio.err)
			continue
		}
		for _, frag := range ChunkAudio(audio.pcm, 8192) {
			if len(frag) == 0 {
				continue
			}
			timing.mark("first_audio_ms")
			select {
			case p.messages <- &Message{Audio: frag}:
			case <-p.closedCh:
				return nil
			case <-ctx.Done():
				failure = ctx.Err()
			}
			if failure != nil {
				break
			}
		}
	}
	return failure
}

// sentenceSplitter cuts a stream of answer deltas into speakable pieces. A
// piece ends at a sentence terminator followed by whitespace; the first piece
// of a turn may also end at a clause boundary so speech starts sooner. A
// terminator at the very end of the buffer waits for the next delta, because
// "3." or "z." may continue.
type sentenceSplitter struct {
	buf     []rune
	emitted bool
}

// Push adds a delta and returns the pieces that are complete.
func (s *sentenceSplitter) Push(delta string) []string {
	s.buf = append(s.buf, []rune(delta)...)
	var out []string
	for {
		cut := s.boundary()
		if cut <= 0 {
			return out
		}
		piece := strings.TrimSpace(string(s.buf[:cut]))
		s.buf = s.buf[cut:]
		if piece == "" {
			continue
		}
		out = append(out, piece)
		s.emitted = true
	}
}

// Flush returns what is left at the end of the answer.
func (s *sentenceSplitter) Flush() string {
	rest := strings.TrimSpace(string(s.buf))
	s.buf = nil
	return rest
}

// boundary returns the rune index after which the buffer can be cut, or 0.
func (s *sentenceSplitter) boundary() int {
	for i := 0; i < len(s.buf); i++ {
		r := s.buf[i]
		switch {
		case r == '\n':
			if s.speakable(i) {
				return i + 1
			}
		case r == '.' || r == '!' || r == '?' || r == '…':
			if i+1 >= len(s.buf) {
				// A full stop at the very end of the buffer may continue ("3.", "z."), so it waits for the next delta.
				// "!", "?" and "…" cannot, so a speakable piece leaves at once instead of waiting for the next delta
				// or for the end of the answer.
				if r != '.' && s.speakable(i) {
					return i + 1
				}
				continue
			}
			if !unicode.IsSpace(s.buf[i+1]) {
				continue
			}
			if r == '.' && s.abbreviation(i) {
				continue
			}
			if s.speakable(i) {
				return i + 1
			}
		case !s.emitted && (r == ',' || r == ';' || r == ':'):
			if i+1 < len(s.buf) && unicode.IsSpace(s.buf[i+1]) && i+1 >= minFirstClauseRunes {
				return i + 1
			}
		}
	}
	if len(s.buf) >= maxSegmentRunes {
		cut := len(s.buf)
		for i := len(s.buf) - 1; i > maxSegmentRunes/2; i-- {
			if unicode.IsSpace(s.buf[i]) {
				cut = i + 1
				break
			}
		}
		return cut
	}
	return 0
}

// speakable reports whether the text up to index end is long enough to stand
// as its own spoken piece.
func (s *sentenceSplitter) speakable(end int) bool {
	min := minSegmentRunes
	if !s.emitted {
		min = minFirstSegmentRunes
	}
	return utf8.RuneCountInString(strings.TrimSpace(string(s.buf[:end+1]))) >= min
}

var spokenAbbreviations = map[string]struct{}{
	"z": {}, "b": {}, "d": {}, "h": {}, "u": {}, "a": {}, "e": {}, "g": {}, "i": {}, "o": {},
	"ca": {}, "bzw": {}, "usw": {}, "etc": {}, "vgl": {}, "evtl": {}, "ggf": {}, "inkl": {}, "max": {}, "min": {},
	"nr": {}, "tel": {}, "dr": {}, "prof": {}, "hr": {}, "fr": {}, "mr": {}, "mrs": {}, "ms": {}, "st": {}, "vs": {},
}

// abbreviation reports whether the full stop at index dot ends an
// abbreviation or an ordinal number rather than a sentence.
func (s *sentenceSplitter) abbreviation(dot int) bool {
	start := dot
	for start > 0 && !unicode.IsSpace(s.buf[start-1]) {
		start--
	}
	token := strings.ToLower(string(s.buf[start:dot]))
	if token == "" {
		return false
	}
	if _, ok := spokenAbbreviations[token]; ok {
		return true
	}
	// An ordinal or a short number before the stop: "am 3. Mai", "Punkt 2.".
	digits := 0
	for _, r := range token {
		if !unicode.IsDigit(r) {
			return false
		}
		digits++
	}
	return digits <= 2
}
