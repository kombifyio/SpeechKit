package wakeword

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// wordBoundary is the SentencePiece word-boundary marker (U+2581, "▁") that
// prefixes word-initial pieces in the sherpa-onnx BPE token vocabularies.
const wordBoundary = "▁"

// EncodeKeywords converts inline raw keyword phrases into the space-separated
// BPE-token format sherpa-onnx keyword spotting expects, using greedy
// longest-match over the model's tokens.txt vocabulary.
//
// Each input line may carry trailing metadata fields (":<boost>",
// "#<threshold>", "@<label>") which are preserved verbatim after the tokens.
// Lines that already contain the word-boundary marker are treated as
// pre-encoded and returned unchanged (idempotent).
//
// Greedy longest-match approximates the true SentencePiece/BPE segmentation and
// is intended for short wake phrases. For long or ambiguous phrases prefer
// generating keywords.txt with the model's bpe.model (see
// examples/kombify-box-satellite/tools/make-keywords.ps1).
func EncodeKeywords(tokensPath string, rawKeywords []string) ([]string, error) {
	vocab, maxLen, err := loadTokenVocab(tokensPath)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rawKeywords))
	for _, raw := range rawKeywords {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		encoded, err := encodeKeywordLine(line, vocab, maxLen)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("wakeword: no keywords to encode")
	}
	return out, nil
}

func encodeKeywordLine(line string, vocab map[string]struct{}, maxLen int) (string, error) {
	// Already tokenized -> pass through unchanged (idempotent).
	if strings.Contains(line, wordBoundary) {
		return line, nil
	}
	fields := strings.Fields(line)
	// Peel trailing metadata fields (":boost", "#threshold", "@label").
	split := len(fields)
	for split > 0 {
		switch fields[split-1][0] {
		case ':', '#', '@':
			split--
			continue
		}
		break
	}
	words := fields[:split]
	suffix := fields[split:]
	if len(words) == 0 {
		return "", fmt.Errorf("wakeword: keyword %q has no phrase to encode", line)
	}
	// Build the SentencePiece surface: uppercase, each word prefixed with ▁.
	var surface strings.Builder
	for _, w := range words {
		surface.WriteString(wordBoundary)
		surface.WriteString(strings.ToUpper(w))
	}
	pieces, err := greedyTokenize(surface.String(), vocab, maxLen)
	if err != nil {
		return "", fmt.Errorf("wakeword: keyword %q: %w", line, err)
	}
	return strings.Join(append(pieces, suffix...), " "), nil
}

// greedyTokenize segments s into the longest vocabulary pieces, left to right.
func greedyTokenize(s string, vocab map[string]struct{}, maxLen int) ([]string, error) {
	runes := []rune(s)
	pieces := make([]string, 0, len(runes))
	for i := 0; i < len(runes); {
		end := i + maxLen
		if end > len(runes) {
			end = len(runes)
		}
		matched := ""
		for j := end; j > i; j-- {
			cand := string(runes[i:j])
			if _, ok := vocab[cand]; ok {
				matched = cand
				i = j
				break
			}
		}
		if matched == "" {
			return nil, fmt.Errorf("cannot encode with the model vocabulary (unknown token near %q)", string(runes[i]))
		}
		pieces = append(pieces, matched)
	}
	return pieces, nil
}

// ValidateKeywordsFile checks that a keywords file looks BPE-tokenized rather
// than raw text. It catches the common silent failure where sherpa-onnx never
// matches because it was handed plain words (e.g. "kombify") instead of the
// tokenized form ("▁COM B IF Y ..."). Leading-'#' lines are treated as comments
// (an inline "#<threshold>" suffix never starts a line).
func ValidateKeywordsFile(path string) error {
	f, err := os.Open(path) // #nosec G304 -- path is the host-configured keywords file; read-only sanity scan, never executed.
	if err != nil {
		return fmt.Errorf("wakeword: open keywords %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	content, tokenized := 0, 0
	firstRaw := ""
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		content++
		if strings.Contains(line, wordBoundary) {
			tokenized++
		} else if firstRaw == "" {
			firstRaw = line
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("wakeword: read keywords %s: %w", path, err)
	}
	if content == 0 {
		return fmt.Errorf("wakeword: keywords file %s has no keyword entries", path)
	}
	if tokenized == 0 {
		return fmt.Errorf("wakeword: keywords file %s appears to be raw text (e.g. %q), not BPE tokens; "+
			"run tools/make-keywords.ps1 or pass inline Keywords (auto-encoded)", path, firstRaw)
	}
	return nil
}

func loadTokenVocab(path string) (map[string]struct{}, int, error) {
	f, err := os.Open(path) // #nosec G304 -- path is the host-configured tokens.txt of the KWS model; read-only vocabulary load.
	if err != nil {
		return nil, 0, fmt.Errorf("wakeword: open tokens %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	vocab := make(map[string]struct{})
	maxLen := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		// tokens.txt lines are "<piece> <id>"; the piece is field 0. The ▁
		// word-boundary rune is not ASCII whitespace, so Fields keeps it intact.
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		piece := fields[0]
		vocab[piece] = struct{}{}
		if n := utf8.RuneCountInString(piece); n > maxLen {
			maxLen = n
		}
	}
	if err := sc.Err(); err != nil {
		return nil, 0, fmt.Errorf("wakeword: read tokens %s: %w", path, err)
	}
	if len(vocab) == 0 {
		return nil, 0, fmt.Errorf("wakeword: tokens file %s is empty", path)
	}
	return vocab, maxLen, nil
}
