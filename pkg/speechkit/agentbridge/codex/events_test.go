package codex

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentbridge"
)

func TestParseExecEvent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		line     string
		wantOK   bool
		wantType agentbridge.EventType
		check    func(t *testing.T, ev agentbridge.Event)
	}{
		{
			name: "thread started carries the id", wantOK: true, wantType: agentbridge.EventThreadStarted,
			line: `{"type":"thread.started","thread_id":"th_1"}`,
			check: func(t *testing.T, ev agentbridge.Event) {
				if ev.ThreadID != "th_1" {
					t.Fatalf("thread id = %q", ev.ThreadID)
				}
			},
		},
		{
			name: "command item summarizes command and exit code", wantOK: true, wantType: agentbridge.EventItemCompleted,
			line: `{"type":"item.completed","item":{"item_type":"command_execution","command":"go vet ./...","exit_code":0}}`,
			check: func(t *testing.T, ev agentbridge.Event) {
				if ev.Item == nil || ev.Item.Kind != "command_execution" {
					t.Fatalf("item = %+v", ev.Item)
				}
				if ev.Item.Summary != "go vet ./... (exit 0)" {
					t.Fatalf("summary = %q", ev.Item.Summary)
				}
			},
		},
		{
			name: "turn failed becomes an error event", wantOK: true, wantType: agentbridge.EventError,
			line: `{"type":"turn.failed","error":{"message":"boom"}}`,
			check: func(t *testing.T, ev agentbridge.Event) {
				if ev.Err != "boom" {
					t.Fatalf("err = %q", ev.Err)
				}
			},
		},
		{name: "unknown types are skipped", line: `{"type":"some.future_event"}`, wantOK: false},
		{name: "non-json noise is skipped", line: `not json`, wantOK: false},
		{name: "blank lines are skipped", line: `   `, wantOK: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, ok := parseExecEvent([]byte(tc.line))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if ev.Type != tc.wantType {
				t.Fatalf("type = %s, want %s", ev.Type, tc.wantType)
			}
			if tc.check != nil {
				tc.check(t, ev)
			}
		})
	}
}
