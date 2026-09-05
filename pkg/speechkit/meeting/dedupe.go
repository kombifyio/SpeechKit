package meeting

import (
	"strings"
	"unicode"
)

// Without a headset, the microphone hears the call coming out of the speakers.
// Both channels then transcribe the same speech, and the write-up ends up
// treating one person's sentence as two — often with the echoed copy garbled,
// because a speaker plus room is a worse microphone than the call itself.
//
// This is measurable: a hardware run of the meeting E2E on a USB speaker+mic
// device transcribed the played fixture cleanly on the loopback channel while
// the microphone returned a mangled version of the same words.
//
// The fix is deliberately deterministic rather than a prompt instruction. An
// echo is the same words at the same time on the opposite channel, which is
// something arithmetic can decide; asking a model to notice it costs tokens and
// gets it wrong on exactly the garbled cases that matter.

const (
	// echoWindowMs is how far apart two segments may start and still be the
	// same speech. Loopback and microphone paths differ by device buffering and
	// the segmenter's own boundaries, not by seconds.
	echoWindowMs = 3000
	// echoSimilarity is how much of the wording two segments must share. Set
	// below the point where an echo is transcribed perfectly, because the
	// echoed copy is usually the degraded one.
	echoSimilarity = 0.6
	// echoMinWords keeps short utterances out of it. "Yes" said by two people
	// at once is two people agreeing, not an echo.
	echoMinWords = 4
)

// SuppressEcho drops the segments that are only the call arriving twice,
// keeping the loopback copy because it heard the call as digital audio.
//
// It never touches what is stored: the transcript keeps both, because that is
// what was captured. Only the write-up is spared the duplicate.
func SuppressEcho(lines []TranscriptLine) []TranscriptLine {
	if len(lines) < 2 {
		return lines
	}
	dropped := make(map[int]bool, len(lines))
	tokens := make([]map[string]struct{}, len(lines))
	counts := make([]int, len(lines))
	for i, line := range lines {
		tokens[i], counts[i] = wordSet(line.Text)
	}

	for i := range lines {
		if dropped[i] || counts[i] < echoMinWords {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if dropped[j] || counts[j] < echoMinWords {
				continue
			}
			if lines[j].StartMs-lines[i].StartMs > echoWindowMs {
				break
			}
			if lines[i].Channel == lines[j].Channel || lines[i].Channel == "" || lines[j].Channel == "" {
				continue
			}
			if jaccard(tokens[i], tokens[j]) < echoSimilarity {
				continue
			}
			// The loopback copy always wins. Speech that shows up on both
			// channels is the call coming back through the speakers, and the
			// loopback heard it as digital audio while the microphone heard a
			// room. The local speaker's own voice is never duplicated: it goes
			// out to the others, not back through the local output.
			if lines[i].Channel == ChannelMicrophone {
				dropped[i] = true
				break
			}
			dropped[j] = true
		}
	}

	out := make([]TranscriptLine, 0, len(lines))
	for i, line := range lines {
		if dropped[i] {
			continue
		}
		out = append(out, line)
	}
	return out
}

func wordSet(text string) (map[string]struct{}, int) {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	set := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		set[field] = struct{}{}
	}
	return set, len(fields)
}

// jaccard is the share of distinct words two segments have in common.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	overlap := 0
	smaller, larger := a, b
	if len(b) < len(a) {
		smaller, larger = b, a
	}
	for word := range smaller {
		if _, ok := larger[word]; ok {
			overlap++
		}
	}
	union := len(a) + len(b) - overlap
	if union == 0 {
		return 0
	}
	return float64(overlap) / float64(union)
}
