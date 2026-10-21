package a2a

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

type scriptedTurns struct{ fail bool }

func (s *scriptedTurns) StreamTurn(_ context.Context, _ cascaded.AgentInput, _ string, emit func(string) error) (cascaded.AgentOutput, error) {
	if err := emit("partial "); err != nil {
		return cascaded.AgentOutput{}, err
	}
	if s.fail {
		return cascaded.AgentOutput{}, errors.New("turn failed after first delta")
	}
	return cascaded.AgentOutput{}, emit("answer")
}

// Regression: an in-process turn that fails after its first delta delivers
// nothing, so the AI disclosure must still lead the next delivered answer.
func TestAnswerDisclosesOnFirstDeliveredAnswer(t *testing.T) {
	agent := &scriptedTurns{fail: true}
	handler, err := NewOpenAIHandler(context.Background(), agent, strings.Repeat("k", 32), time.Now().Add(time.Minute), "en")
	if err != nil {
		t.Fatal(err)
	}
	handler.FirstResponsePrefix = "Notice: AI. "
	finals := NewFinalTurnBinding("session")
	handler.BindTurn = finals.Resolve
	finals.Observe("first", "1")
	if _, err := handler.Answer(context.Background(), "first"); err == nil {
		t.Fatal("failed turn produced an answer")
	}
	agent.fail = false
	finals.Observe("second", "2")
	answer, err := handler.Answer(context.Background(), "second")
	if err != nil || !strings.HasPrefix(answer, "Notice: AI. ") {
		t.Fatalf("delivered answer %q lost the disclosure: %v", answer, err)
	}
	finals.Observe("third", "3")
	if again, _ := handler.Answer(context.Background(), "third"); strings.HasPrefix(again, "Notice") {
		t.Fatal("disclosure repeated after it was delivered")
	}
}
