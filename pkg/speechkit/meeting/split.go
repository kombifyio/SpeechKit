package meeting

import (
	"strings"
	"unicode/utf8"
)

// MaxTranscriptLineRunes bounds one transcript line as the models see it. A
// meeting channel that never pauses used to arrive as one 19,000-character
// segment (2026-09-03); no local context window holds that in a single
// request, and the summary batches and the write-up chunker can only split
// between lines, never inside one. Lines longer than this are cut into parts
// at sentence boundaries. The value is fixed rather than derived from the
// model so the same segment always yields the same parts.
const MaxTranscriptLineRunes = 1800

// SplitLongLines returns lines with every over-long line replaced by parts
// that fit MaxTranscriptLineRunes. Parts keep the segment id, speaker,
// channel and start time of the original, so citations still point at the
// segment and the parts stay in order.
func SplitLongLines(lines []TranscriptLine, maxRunes int) []TranscriptLine {
	if maxRunes <= 0 {
		maxRunes = MaxTranscriptLineRunes
	}
	out := make([]TranscriptLine, 0, len(lines))
	for _, line := range lines {
		if utf8.RuneCountInString(line.Text) <= maxRunes {
			out = append(out, line)
			continue
		}
		for _, part := range splitText(line.Text, maxRunes) {
			copied := line
			copied.Text = part
			out = append(out, copied)
		}
	}
	return out
}

// splitText cuts text into pieces of at most maxRunes runes, preferring a
// sentence end in the second half of the window, then any space, then a hard
// cut so a run without spaces cannot defeat the limit.
func splitText(text string, maxRunes int) []string {
	runes := []rune(strings.TrimSpace(text))
	parts := make([]string, 0, len(runes)/maxRunes+1)
	for len(runes) > maxRunes {
		cut := sentenceCut(runes, maxRunes)
		if cut <= 0 {
			cut = spaceCut(runes, maxRunes)
		}
		if cut <= 0 {
			cut = maxRunes
		}
		part := strings.TrimSpace(string(runes[:cut]))
		if part != "" {
			parts = append(parts, part)
		}
		runes = []rune(strings.TrimSpace(string(runes[cut:])))
	}
	if rest := strings.TrimSpace(string(runes)); rest != "" {
		parts = append(parts, rest)
	}
	return parts
}

// sentenceCut is the position after the last sentence end inside the window,
// if one lies in its second half.
func sentenceCut(runes []rune, maxRunes int) int {
	minimum := maxRunes / 2
	for index := maxRunes - 1; index > minimum; index-- {
		switch runes[index] {
		case '.', '!', '?', '…', ';':
			if index+1 < len(runes) && runes[index+1] == ' ' {
				return index + 1
			}
		}
	}
	return 0
}

// spaceCut is the last space inside the window, if one lies in its second half.
func spaceCut(runes []rune, maxRunes int) int {
	minimum := maxRunes / 2
	for index := maxRunes - 1; index > minimum; index-- {
		if runes[index] == ' ' {
			return index
		}
	}
	return 0
}
