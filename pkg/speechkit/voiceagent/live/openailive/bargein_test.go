package openailive

import (
	"fmt"
	"testing"
)

// GPT-Live has no barge-in event, so user speech that starts before the
// assistant's audio ends on the session timeline must flush playback once per
// assistant output run, and a later run must be interruptible again.
func TestUserSpeechOverAssistantOutputIsBargeIn(t *testing.T) {
	p := New()
	event := func(typ string, start, end int64) bool {
		t.Helper()
		msg, err := p.parseEvent([]byte(fmt.Sprintf(`{"type":%q,"delta":"x","start_ms":%d,"end_ms":%d}`, typ, start, end)))
		if err != nil {
			t.Fatalf("parseEvent: %v", err)
		}
		return msg.Interrupted
	}
	for _, step := range []struct {
		output     bool
		start, end int64
		want       bool
	}{
		{output: true, start: 0, end: 2000},
		{start: 2100, end: 2200, want: false}, // after the assistant finished
		{output: true, start: 2500, end: 4000},
		{start: 3000, end: 3100, want: true},  // user talks over the assistant
		{start: 3200, end: 3300, want: false}, // same barge-in, already flushed
		{output: true, start: 4500, end: 6000},
		{start: 5000, end: 5100, want: true}, // a new run is interruptible again
	} {
		if step.output {
			event("session.output_transcript.delta", step.start, step.end)
			continue
		}
		if got := event("session.input_transcript.delta", step.start, step.end); got != step.want {
			t.Fatalf("input at %d ms: interrupted = %v, want %v", step.start, got, step.want)
		}
	}
}
