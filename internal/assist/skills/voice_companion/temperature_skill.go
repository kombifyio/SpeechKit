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

// TemperatureSkill converts a temperature between Celsius, Fahrenheit, and
// Kelvin — a very common voice-companion query ("convert 20 celsius to
// fahrenheit", "wandle 100 grad celsius in kelvin"). Deterministic and fully
// in-process; no external service.
//
// It expects the payload to name BOTH a source and a target unit, separated by
// "to" / "in" / "nach" (e.g. "20 celsius to fahrenheit"). Anything it cannot
// parse into value + source + target comes back silent so the host falls
// through to the LLM.
type TemperatureSkill struct{}

// NewTemperatureSkill returns a ready TemperatureSkill.
func NewTemperatureSkill() *TemperatureSkill { return &TemperatureSkill{} }

// Intent reports the shortcuts.Intent this skill handles.
func (s *TemperatureSkill) Intent() shortcuts.Intent { return shortcuts.IntentTemperature }

// Execute runs the skill against a single ToolCall.
func (s *TemperatureSkill) Execute(_ context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	value, src, dst, ok := parseTemperatureRequest(call.Payload)
	if !ok {
		// Retry with the full transcript in case the resolver stripped a
		// leading unit-less prefix.
		if value, src, dst, ok = parseTemperatureRequest(call.Transcript); !ok {
			return silentResult(call.Locale), nil
		}
	}

	result := convertTemperature(value, src, dst)
	text := formatTemperature(value, src, result, dst, call.Locale)
	return assist.ToolResult{
		Text:      text,
		SpeakText: text,
		Action:    "respond",
		Locale:    call.Locale,
		Surface:   assist.ResultSurfacePanel,
		Kind:      assist.ResultKindAnswer,
	}, nil
}

var (
	temperatureConnectors = []string{" to ", " in ", " nach ", " into "}
	temperatureNumber     = regexp.MustCompile(`[-+]?\d+(?:[.,]\d+)?`)
)

// parseTemperatureRequest extracts (value, sourceUnit, targetUnit) from a phrase
// like "20 celsius to fahrenheit". Units are "C", "F", or "K".
func parseTemperatureRequest(payload string) (value float64, src, dst string, ok bool) {
	expr := strings.ToLower(strings.TrimSpace(payload))
	if expr == "" {
		return 0, "", "", false
	}

	left, right, found := splitOnAny(expr, temperatureConnectors)
	if !found {
		return 0, "", "", false
	}

	src, ok = detectTemperatureUnit(left)
	if !ok {
		return 0, "", "", false
	}
	dst, ok = detectTemperatureUnit(right)
	if !ok {
		return 0, "", "", false
	}

	m := temperatureNumber.FindString(left)
	if m == "" {
		return 0, "", "", false
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(m, ",", "."), 64)
	if err != nil {
		return 0, "", "", false
	}
	return value, src, dst, true
}

// splitOnAny splits s on the first connector that appears, returning the left
// and right sides.
func splitOnAny(s string, connectors []string) (left, right string, found bool) {
	idx := -1
	for _, c := range connectors {
		if i := strings.Index(s, c); i >= 0 && (idx < 0 || i < idx) {
			idx = i
			left = strings.TrimSpace(s[:i])
			right = strings.TrimSpace(s[i+len(c):])
		}
	}
	return left, right, idx >= 0
}

// detectTemperatureUnit recognises the scale named in a fragment. Fahrenheit
// and Kelvin are checked before Celsius because "grad"/"degrees" default to
// Celsius only when no more specific scale is named.
func detectTemperatureUnit(s string) (string, bool) {
	s = " " + s + " "
	switch {
	case strings.Contains(s, "fahrenheit"), strings.Contains(s, "°f"), strings.Contains(s, " f "):
		return "F", true
	case strings.Contains(s, "kelvin"), strings.Contains(s, "°k"), strings.Contains(s, " k "):
		return "K", true
	case strings.Contains(s, "celsius"), strings.Contains(s, "°c"), strings.Contains(s, " c "), strings.Contains(s, "grad"), strings.Contains(s, "degree"):
		return "C", true
	}
	return "", false
}

// convertTemperature converts value from src to dst via Celsius as the pivot.
func convertTemperature(value float64, src, dst string) float64 {
	var c float64
	switch src {
	case "F":
		c = (value - 32) * 5 / 9
	case "K":
		c = value - 273.15
	default:
		c = value
	}
	switch dst {
	case "F":
		return c*9/5 + 32
	case "K":
		return c + 273.15
	default:
		return c
	}
}

// formatTemperature renders "20 °C is 68 °F." (EN) / "20 °C sind 68 °F." (DE).
func formatTemperature(value float64, src string, result float64, dst, locale string) string {
	from := fmt.Sprintf("%s %s", trimFloat(value), symbolFor(src))
	to := fmt.Sprintf("%s %s", trimFloat(result), symbolFor(dst))
	if normalizeLocale(locale) == "de" {
		return fmt.Sprintf("%s sind %s.", from, to)
	}
	return fmt.Sprintf("%s is %s.", from, to)
}

func symbolFor(unit string) string {
	switch unit {
	case "F":
		return "°F"
	case "K":
		return "K"
	default:
		return "°C"
	}
}

// trimFloat renders a temperature with up to two decimal places, dropping
// trailing zeros so whole numbers read naturally ("68", not "68.00") while
// exact values like 273.15 K keep their precision.
func trimFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}
