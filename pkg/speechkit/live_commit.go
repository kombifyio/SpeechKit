package speechkit

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// LiveCommitImmediate pastes each provider-final as soon as it arrives.
	LiveCommitImmediate = "immediate"
	// LiveCommitPhrase waits for one sentence (or a short pause) before paste.
	LiveCommitPhrase = "phrase"
	// LiveCommitPassage waits for about two sentences (or a longer pause) so
	// live field injection reads as prose rather than breath-sized fragments.
	LiveCommitPassage = "passage"
)

const (
	liveCommitPhraseHold  = 600 * time.Millisecond
	liveCommitPassageHold = 1600 * time.Millisecond
	liveCommitPhraseMin   = 1
	liveCommitPassageMin  = 2
)

// LiveCommitPolicy groups provider-native finals before they reach a sink.
// Overlay drafts still pass through immediately.
type LiveCommitPolicy struct {
	Mode         string
	MinSentences int
	Hold         time.Duration
}

// NormalizeLiveCommitPolicy maps a host mode onto hold/sentence defaults.
// Empty or unknown modes disable grouping so existing hosts stay immediate.
func NormalizeLiveCommitPolicy(mode string) LiveCommitPolicy {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case LiveCommitImmediate:
		return LiveCommitPolicy{Mode: LiveCommitImmediate, MinSentences: 1, Hold: 0}
	case LiveCommitPhrase:
		return LiveCommitPolicy{Mode: LiveCommitPhrase, MinSentences: liveCommitPhraseMin, Hold: liveCommitPhraseHold}
	case LiveCommitPassage:
		return LiveCommitPolicy{Mode: LiveCommitPassage, MinSentences: liveCommitPassageMin, Hold: liveCommitPassageHold}
	default:
		return LiveCommitPolicy{}
	}
}

func (p LiveCommitPolicy) groupsFinals() bool {
	return p.Mode != "" && p.Mode != LiveCommitImmediate
}

// LiveCommitFlusher drains a grouped live-commit buffer. Recording stop calls
// this so a trailing sentence is not left behind when the stream ends.
type LiveCommitFlusher interface {
	FlushLiveCommit(ctx context.Context) error
}

type liveCommitSink struct {
	inner  DictationStreamSink
	policy LiveCommitPolicy

	mu    sync.Mutex
	parts []DictationStreamEvent
	opts  DictationStreamSinkOptions
	timer *time.Timer
}

func wrapLiveCommitSink(inner DictationStreamSink, mode string) DictationStreamSink {
	policy := NormalizeLiveCommitPolicy(mode)
	if inner == nil || !policy.groupsFinals() {
		return inner
	}
	return &liveCommitSink{inner: inner, policy: policy}
}

func (s *liveCommitSink) HandleDictationStreamEvent(ctx context.Context, event DictationStreamEvent, opts DictationStreamSinkOptions) error {
	if s == nil || s.inner == nil {
		return nil
	}
	if !event.IsFinal {
		return s.inner.HandleDictationStreamEvent(ctx, event, opts)
	}
	s.mu.Lock()
	s.parts = append(s.parts, event)
	s.opts = opts
	if s.readyLocked() {
		out, outOpts, ok := s.takeLocked()
		s.mu.Unlock()
		if !ok {
			return nil
		}
		return s.inner.HandleDictationStreamEvent(ctx, out, outOpts)
	}
	s.armHoldLocked()
	s.mu.Unlock()
	return nil
}

func (s *liveCommitSink) FlushLiveCommit(ctx context.Context) error {
	if s == nil || s.inner == nil {
		return nil
	}
	s.mu.Lock()
	out, opts, ok := s.takeLocked()
	s.mu.Unlock()
	if !ok {
		return nil
	}
	return s.inner.HandleDictationStreamEvent(ctx, out, opts)
}

func (s *liveCommitSink) readyLocked() bool {
	minSentences := s.policy.MinSentences
	if minSentences <= 0 {
		minSentences = 1
	}
	return CountTerminalSentences(JoinTranscriptFragments(liveCommitTexts(s.parts)...)) >= minSentences
}

func (s *liveCommitSink) armHoldLocked() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.policy.Hold <= 0 {
		return
	}
	s.timer = time.AfterFunc(s.policy.Hold, func() {
		_ = s.FlushLiveCommit(context.Background())
	})
}

func (s *liveCommitSink) takeLocked() (DictationStreamEvent, DictationStreamSinkOptions, bool) {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if len(s.parts) == 0 {
		return DictationStreamEvent{}, DictationStreamSinkOptions{}, false
	}
	out := coalesceDictationFinals(s.parts)
	opts := s.opts
	s.parts = nil
	return out, opts, true
}

func liveCommitTexts(parts []DictationStreamEvent) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, part.Text)
	}
	return out
}

func coalesceDictationFinals(parts []DictationStreamEvent) DictationStreamEvent {
	out := parts[len(parts)-1]
	var words []WordConfidence
	for _, part := range parts {
		words = append(words, part.Words...)
	}
	out.Text = JoinTranscriptFragments(liveCommitTexts(parts)...)
	out.Words = words
	out.IsFinal = true
	return out
}

// JoinTranscriptFragments concatenates live transcript slices with a single
// separating space when the next slice would otherwise glue onto the previous
// word or sentence.
func JoinTranscriptFragments(parts ...string) string {
	var b strings.Builder
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if b.Len() == 0 {
			b.WriteString(part)
			continue
		}
		if NeedsLiveInjectSpace(b.String(), part) {
			b.WriteByte(' ')
		}
		b.WriteString(part)
	}
	return b.String()
}

// LiveInjectFragment returns the text that should be pasted for this fragment
// so consecutive live injects keep a word gap without rewriting earlier text.
func LiveInjectFragment(previousSession uint64, previousTail, next string, session uint64) (fragment, tail string, nextSession uint64) {
	trimmed := strings.TrimSpace(next)
	if trimmed == "" {
		return "", previousTail, previousSession
	}
	if session == 0 || session != previousSession {
		return next, trimmed, session
	}
	if hasLeadingWhitespace(next) || !NeedsLiveInjectSpace(previousTail, trimmed) {
		return next, trimmed, session
	}
	return " " + next, trimmed, session
}

func hasLeadingWhitespace(s string) bool {
	if s == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsSpace(r)
}

// NeedsLiveInjectSpace reports whether two adjacent live fragments need a
// separating space when pasted one after the other.
func NeedsLiveInjectSpace(prev, next string) bool {
	prev = strings.TrimSpace(prev)
	next = strings.TrimSpace(next)
	if prev == "" || next == "" {
		return false
	}
	nextFirst, _ := utf8.DecodeRuneInString(next)
	if nextFirst == utf8.RuneError || unicode.IsSpace(nextFirst) || isNoSpacePrefix(nextFirst) {
		return false
	}
	prevLast, _ := utf8.DecodeLastRuneInString(prev)
	return !unicode.IsSpace(prevLast)
}

func isNoSpacePrefix(r rune) bool {
	switch r {
	case ',', ';', ':', '.', '!', '?', ')', ']', '}', '%', '…':
		return true
	default:
		return false
	}
}

// CountTerminalSentences counts fragments that already look like finished
// sentences so live commit can wait for a short paragraph instead of a breath.
func CountTerminalSentences(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	count := 0
	start := 0
	runes := []rune(text)
	for i, r := range runes {
		if !isSentenceTerminator(r) {
			continue
		}
		if i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) && !isSentenceTerminator(runes[i+1]) {
			continue
		}
		if strings.TrimSpace(string(runes[start:i+1])) != "" {
			count++
		}
		start = i + 1
	}
	return count
}

func isSentenceTerminator(r rune) bool {
	switch r {
	case '.', '!', '?', '…':
		return true
	default:
		return false
	}
}
