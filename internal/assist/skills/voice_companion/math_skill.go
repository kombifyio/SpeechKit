package voice_companion

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

// MathSkill evaluates a single arithmetic expression spoken by the user.
// Both spoken word-form operators ("mal", "plus", "minus", "durch",
// "geteilt durch", "multipliziert mit", "times") and digit-form
// operators ("*", "+", "-", "/") are accepted. Locale "de" prefers the
// German word forms; "en" the English ones. Decimal payloads with a
// comma ("3,14") are normalised to dot-form before parsing so DE/EN
// number conventions both work.
//
// The skill is intentionally narrow: a single binary operation
// ("15 mal 8", "100 minus 7") OR a unary number ("zweiundvierzig" → not
// supported, requires word-to-number; "42" → echoed as 42). More
// complex expressions ("(15+8) * 2", "sqrt(16)") fall through to the
// LLM via Action="silent". This keeps the skill deterministic and
// fast — the LLM picks up the slack on anything non-trivial.
type MathSkill struct{}

// NewMathSkill returns a fresh MathSkill. The struct carries no state.
func NewMathSkill() *MathSkill { return &MathSkill{} }

// Intent reports the shortcuts.Intent this skill handles.
func (s *MathSkill) Intent() shortcuts.Intent { return shortcuts.IntentMath }

// Execute runs the skill against a single ToolCall. The expression is
// taken from call.Payload (the substring after the matched lexicon
// prefix); if the payload is empty (no prefix match consumed text), we
// fall back to the whole transcript.
func (s *MathSkill) Execute(_ context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	expr := strings.TrimSpace(call.Payload)
	if expr == "" {
		expr = strings.TrimSpace(call.Transcript)
	}
	if expr == "" {
		return silentResult(call.Locale), nil
	}

	result, ok := EvaluateExpression(expr)
	if !ok {
		// Not parseable as a simple math expression — return silent so
		// the Assist pipeline can fall through to the LLM. Returning an
		// error here would surface as a user-visible failure, which is
		// the wrong UX for "I don't know, let the LLM try".
		return silentResult(call.Locale), nil
	}

	text := formatMathAnswer(result, call.Locale)
	return assist.ToolResult{
		Text:      text,
		SpeakText: text,
		Action:    "respond",
		Locale:    call.Locale,
		Surface:   assist.ResultSurfacePanel,
		Kind:      assist.ResultKindAnswer,
	}, nil
}

// EvaluateExpression parses an arithmetic expression in either spoken
// (DE/EN word operators) or digit form and returns the numeric result.
// Returns (0, false) when the input is not a recognisable single binary
// operation or unary number.
//
// Exported so future skills (e.g. timer parsing "5 mal 60" if we ever
// need it) can reuse the same parser without copying.
func EvaluateExpression(expr string) (float64, bool) {
	expr = normalizeMathOperators(expr)
	// Replace decimal comma with dot before stripping non-math chars.
	// Keep this BEFORE the sanitiser, otherwise "3,14" becomes "3 14".
	expr = strings.ReplaceAll(expr, ",", ".")
	sanitized := mathSanitizer.ReplaceAllString(expr, " ")
	sanitized = strings.TrimSpace(collapseWhitespace.ReplaceAllString(sanitized, " "))
	if sanitized == "" {
		return 0, false
	}

	fields := strings.Fields(sanitized)

	// Unary number ("42").
	if len(fields) == 1 {
		v, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			return 0, false
		}
		return v, true
	}

	// Binary operation: <number> <op> <number>. Anything more complex
	// returns false so the LLM picks it up.
	if len(fields) == 3 {
		a, errA := strconv.ParseFloat(fields[0], 64)
		b, errB := strconv.ParseFloat(fields[2], 64)
		if errA != nil || errB != nil {
			return 0, false
		}
		switch fields[1] {
		case "+":
			return a + b, true
		case "-":
			return a - b, true
		case "*":
			return a * b, true
		case "/":
			if b == 0 {
				return 0, false
			}
			return a / b, true
		}
	}

	return 0, false
}

// mathSanitizer strips characters that are not part of a math
// expression (letters, punctuation other than allowed operators).
// Spaces, digits, dots and the four operator symbols survive.
var mathSanitizer = regexp.MustCompile(`[^0-9+\-*/.\s]+`)

// collapseWhitespace turns any run of whitespace into a single space
// so strings.Fields gets a clean tokeniser input.
var collapseWhitespace = regexp.MustCompile(`\s+`)

// normalizeMathOperators replaces spoken operator words with their
// digit-form symbols. Order matters — multi-word phrases are replaced
// before single words so "geteilt durch" doesn't collide with " durch ".
func normalizeMathOperators(expr string) string {
	expr = " " + strings.ToLower(expr) + " "
	replacements := []struct{ from, to string }{
		{" geteilt durch ", " / "},
		{" multipliziert mit ", " * "},
		{" times ", " * "},
		{" divided by ", " / "},
		{" plus ", " + "},
		{" minus ", " - "},
		{" mal ", " * "},
		{" durch ", " / "},
	}
	for _, r := range replacements {
		expr = strings.ReplaceAll(expr, r.from, r.to)
	}
	return strings.TrimSpace(expr)
}

// formatMathAnswer renders the numeric result for the user, choosing
// integer formatting when the value rounds cleanly to a whole number
// (so "15 mal 8" speaks "120", not "120.00") and DE-locale comma form
// otherwise.
func formatMathAnswer(result float64, locale string) string {
	intRounded := int64(result)
	if float64(intRounded) == result {
		switch normalizeLocale(locale) {
		case "de":
			return fmt.Sprintf("Das Ergebnis ist %d.", intRounded)
		default:
			return fmt.Sprintf("The result is %d.", intRounded)
		}
	}
	switch normalizeLocale(locale) {
	case "de":
		formatted := strings.ReplaceAll(fmt.Sprintf("%.2f", result), ".", ",")
		return fmt.Sprintf("Das Ergebnis ist %s.", formatted)
	default:
		return fmt.Sprintf("The result is %.2f.", result)
	}
}

// silentResult is the conventional "I matched the intent but cannot
// answer this specific payload" shape used by skills that defer to the
// LLM. The Assist pipeline checks Action="silent" and falls through to
// the LLM flow rather than speaking nothing.
func silentResult(locale string) assist.ToolResult {
	return assist.ToolResult{
		Action:  "silent",
		Locale:  locale,
		Surface: assist.ResultSurfaceSilent,
		Kind:    assist.ResultKindAnswer,
	}
}
