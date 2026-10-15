package flowadapt

import (
	"context"
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/app/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

func TestNewCascadedAgentMapsInputAndOutput(t *testing.T) {
	var got flows.AgentInput
	flow := flows.New(func(_ context.Context, in flows.AgentInput) (flows.AgentOutput, error) {
		got = in
		return flows.AgentOutput{Text: "hi", Action: "display"}, nil
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
	want := flows.AgentInput{
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
	flow := flows.New(func(context.Context, flows.AgentInput) (flows.AgentOutput, error) {
		return flows.AgentOutput{}, boom
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
