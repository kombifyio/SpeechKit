package sherpa

import "strings"

// Config configures engine.NewRecognizer.
type Config struct {
	// ModelDir is the absolute path of a nemo_transducer model directory
	// (see [ResolveModelDir]).
	ModelDir string
	// Threads is the number of CPU threads the model runs on; values below 1
	// use one thread.
	Threads int
}

// Result is the transcript of one recording.
type Result struct {
	Text string
	// Segments are the spans the recording was decoded in, in order, with
	// their times in seconds. A recording that fits one pass has one.
	Segments []Segment
	// Duration is the audio length in seconds.
	Duration float64
	// Words are the recognised words with their times in seconds, in order.
	Words []Word
}

// Word is one recognised word. Start is when the decoder emitted its first
// token; End is when the next word began, or a short span after the last
// token of the final word.
type Word struct {
	Text  string
	Start float64
	End   float64
}

// lastWordTail is how long the final word of a span is assumed to last past
// its last token's start time.
const lastWordTail = 0.2

// WordsFromTokens groups the tokens of one decoded span into words. A token
// that starts with a space (the SentencePiece word marker) begins a new
// word; the others, punctuation included, continue the current one. starts
// are the tokens' start times in seconds within the span, offset is the span's
// start within the recording and end its end (0 for none).
func WordsFromTokens(tokens []string, starts []float32, offset, end float64) []Word {
	var words []Word
	for i, tok := range tokens {
		if i >= len(starts) {
			break
		}
		at := offset + float64(starts[i])
		if len(words) == 0 || strings.HasPrefix(tok, " ") {
			if n := len(words); n > 0 {
				words[n-1].End = at
			}
			words = append(words, Word{Start: at})
		}
		words[len(words)-1].Text += strings.TrimPrefix(tok, " ")
	}
	if n := len(words); n > 0 {
		last := offset + float64(starts[min(len(tokens), len(starts))-1]) + lastWordTail
		if end > 0 {
			last = min(last, end)
		}
		words[n-1].End = max(last, words[n-1].Start)
	}
	return words
}

// Segment is the transcript of one decoded span.
type Segment struct {
	Start float64
	End   float64
	Text  string
}
