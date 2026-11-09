package provideropts

import (
	"strings"
	"unicode/utf8"
)

// KeytermLimits bounds what one provider's keyterm field accepts. A zero
// field means "no bound of that kind".
type KeytermLimits struct {
	// MaxTerms caps the number of terms.
	MaxTerms int
	// MaxChars drops (never truncates) a term longer than this many runes.
	MaxChars int
	// TokenBudget caps the estimated tokens across all terms
	// ([KeytermTokenEstimate]); a term that no longer fits is skipped.
	TokenBudget int
}

// Documented keyterm bounds of the hosted providers. Past Deepgram's limit of
// 500 tokens Listen and the Voice Agent reject the whole request, so its
// budget stays below it; AssemblyAI accepts at most 100 terms of 50 characters.
var (
	DeepgramKeytermLimits   = KeytermLimits{MaxTerms: 100, TokenBudget: 450}
	AssemblyAIKeytermLimits = KeytermLimits{MaxTerms: 100, MaxChars: 50}
	XAIKeytermLimits        = KeytermLimits{MaxTerms: 100, MaxChars: 50}
)

// CapKeyterms returns terms in order, trimmed, de-duplicated
// case-insensitively and within limits.
func CapKeyterms(terms []string, limits KeytermLimits) []string {
	if len(terms) == 0 {
		return nil
	}
	out := make([]string, 0, len(terms))
	seen := make(map[string]struct{}, len(terms))
	budget := limits.TokenBudget
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" || (limits.MaxChars > 0 && utf8.RuneCountInString(term) > limits.MaxChars) {
			continue
		}
		key := strings.ToLower(term)
		if _, ok := seen[key]; ok {
			continue
		}
		if limits.TokenBudget > 0 {
			cost := KeytermTokenEstimate(term)
			if cost > budget {
				continue
			}
			budget -= cost
		}
		seen[key] = struct{}{}
		out = append(out, term)
		if limits.MaxTerms > 0 && len(out) >= limits.MaxTerms {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// KeytermTokenEstimate over-estimates the tokens a provider counts for one
// keyterm. Measured against Deepgram (2026-11-02): short terms cost about 7
// tokens and longer ones about 4 plus 0.4 per character, so the estimate is
// at least 8 and 4 plus half a token per character.
func KeytermTokenEstimate(term string) int {
	return max(8, 4+(utf8.RuneCountInString(term)+1)/2)
}
