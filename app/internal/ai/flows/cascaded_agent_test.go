package flows

import (
	"context"
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

func TestNewCascadedAgentMapsInputAndOutput(t *testing.T) {
	var got AgentInput
	flow := New(func(_ context.Context, in AgentInput) (AgentOutput, error) {
		got = in
		return AgentOutput{Text: "hi", Action: "display"}, nil
	})

	out, err := NewCascadedAgent(flow).Run(context.Background(), cascaded.AgentInput{
		Utterance:         "what is my name",
		Locale:            "de",
		Selection:         "sel",
		LastTranscription: "history",
		SystemPrompt:      "be brief",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := AgentInput{
		Utterance:         "what is my name",
		Locale:            "de",
		Selection:         "sel",
		LastTranscription: "history",
		SystemPrompt:      "be brief",
	}
	if got != want {
		t.Fatalf("flow input = %+v, want %+v", got, want)
	}
	if out.Text != "hi" || out.Action != "display" {
		t.Fatalf("output = %+v, want Text=hi Action=display", out)
	}
}

func TestNewCascadedAgentPropagatesFlowError(t *testing.T) {
	boom := errors.New("boom")
	flow := New(func(context.Context, AgentInput) (AgentOutput, error) {
		return AgentOutput{}, boom
	})
	if _, err := NewCascadedAgent(flow).Run(context.Background(), cascaded.AgentInput{}); !errors.Is(err, boom) {
		t.Fatalf("Run error = %v, want %v", err, boom)
	}
}

func TestNewCascadedAgentRejectsNilFlow(t *testing.T) {
	if _, err := NewCascadedAgent(nil).Run(context.Background(), cascaded.AgentInput{}); err == nil {
		t.Fatal("expected an error for a nil flow")
	}
}
