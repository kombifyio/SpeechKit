package toolbridge

import (
	"context"
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
)

type fakeSkill struct {
	matched  bool
	err      error
	request  speechkit.AssistRequest
	executed *assist.ToolCall
	result   assist.ToolResult
}

func (f *fakeSkill) MatchTool(_ context.Context, req speechkit.AssistRequest) (assist.ToolCall, bool, error) {
	f.request = req
	if f.err != nil {
		return assist.ToolCall{}, false, f.err
	}
	if !f.matched {
		return assist.ToolCall{}, false, nil
	}
	return assist.ToolCall{Intent: "home_assistant", Payload: req.Text, Locale: req.Locale}, true, nil
}

func (f *fakeSkill) ExecuteTool(_ context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	f.executed = &call
	return f.result, nil
}

func TestNewValidatesOptions(t *testing.T) {
	if _, err := New(Options{Name: "x", Description: "y"}); !errors.Is(err, ErrMissingExecutor) {
		t.Fatalf("missing executor = %v", err)
	}
	if _, err := New(Options{Matcher: &fakeSkill{}, Executor: &fakeSkill{}}); err == nil {
		t.Fatal("missing name/description should error")
	}
}

func TestInvokeMatchedExecutes(t *testing.T) {
	skill := &fakeSkill{matched: true, result: assist.ToolResult{
		Text:      "Licht ist an.",
		SpeakText: "Licht ist an.",
		Action:    "execute",
		Kind:      "utility_action",
	}}
	tool, err := New(Options{
		Name:          "home_assistant",
		Description:   "Steuert das Smart Home.",
		Matcher:       skill,
		Executor:      skill,
		DefaultLocale: "de-DE",
		SessionKey:    "kbx:KBX-0001",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out, err := tool.Invoke(context.Background(), map[string]any{"query": "schalte das Licht an"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out["matched"] != true || out["text"] != "Licht ist an." || out["speak_hint"] != "Licht ist an." {
		t.Fatalf("out = %+v", out)
	}
	if skill.request.Locale != "de-DE" || skill.request.SessionKey != "kbx:KBX-0001" {
		t.Fatalf("request = %+v", skill.request)
	}
	if skill.executed == nil || skill.executed.Payload != "schalte das Licht an" {
		t.Fatalf("executed = %+v", skill.executed)
	}
}

func TestInvokeUnmatchedTellsAgentToAnswerItself(t *testing.T) {
	skill := &fakeSkill{matched: false}
	tool, err := New(Options{Name: "home_assistant", Description: "d", Matcher: skill, Executor: skill})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out, err := tool.Invoke(context.Background(), map[string]any{"query": "wer war Einstein?"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out["matched"] != false {
		t.Fatalf("out = %+v", out)
	}
	if skill.executed != nil {
		t.Fatal("unmatched query must not execute")
	}
}

func TestInvokeLocaleArgumentWins(t *testing.T) {
	skill := &fakeSkill{matched: true}
	tool, err := New(Options{Name: "n", Description: "d", Matcher: skill, Executor: skill, DefaultLocale: "de-DE"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := tool.Invoke(context.Background(), map[string]any{"query": "turn on", "locale": "en-US"}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if skill.request.Locale != "en-US" {
		t.Fatalf("locale = %q", skill.request.Locale)
	}
}

func TestInvokeEmptyQuery(t *testing.T) {
	skill := &fakeSkill{}
	tool, err := New(Options{Name: "n", Description: "d", Matcher: skill, Executor: skill})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out, err := tool.Invoke(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out["matched"] != false {
		t.Fatalf("out = %+v", out)
	}
}
