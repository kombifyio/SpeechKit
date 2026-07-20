package voice_companion

import (
	"context"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/assist"
)

func TestTemperatureSkillConverts(t *testing.T) {
	skill := NewTemperatureSkill()
	cases := []struct {
		name    string
		payload string
		locale  string
		want    string // substring the spoken result must contain
	}{
		{"celsius to fahrenheit", "20 celsius to fahrenheit", "en", "68 °F"},
		{"fahrenheit to celsius", "68 fahrenheit to celsius", "en", "20 °C"},
		{"celsius to kelvin", "0 celsius to kelvin", "en", "273.15 K"},
		{"kelvin to celsius", "273.15 kelvin to celsius", "en", "0 °C"},
		{"decimal + rounding", "21 celsius to fahrenheit", "en", "69.8 °F"},
		{"german grad in fahrenheit", "20 grad celsius in fahrenheit", "de", "68 °F"},
		{"german uses sind", "20 celsius in fahrenheit", "de", "sind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := skill.Execute(context.Background(), assist.ToolCall{Payload: tc.payload, Locale: tc.locale})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if res.Surface == assist.ResultSurfaceSilent {
				t.Fatalf("unexpected silent result for %q", tc.payload)
			}
			if !strings.Contains(res.Text, tc.want) {
				t.Errorf("result = %q, want it to contain %q", res.Text, tc.want)
			}
		})
	}
}

func TestTemperatureSkillSilentWhenUnparseable(t *testing.T) {
	skill := NewTemperatureSkill()
	for _, payload := range []string{
		"",                    // empty
		"the weather is nice", // no number, no units, no connector
		"20 celsius",          // no target unit
		"celsius to kelvin",   // no value
	} {
		res, err := skill.Execute(context.Background(), assist.ToolCall{Payload: payload, Locale: "en"})
		if err != nil {
			t.Fatalf("Execute(%q): %v", payload, err)
		}
		if res.Surface != assist.ResultSurfaceSilent {
			t.Errorf("payload %q surface = %q, want silent so the host can use the LLM", payload, res.Surface)
		}
	}
}
