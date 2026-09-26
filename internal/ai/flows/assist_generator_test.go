package flows

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
)

func TestAssistGeneratorNilFlowIsNilGenerator(t *testing.T) {
	if got := AssistGenerator(nil); got != nil {
		t.Fatalf("AssistGenerator(nil) = %#v, want a nil Generator so Service.HasGenerator reports false", got)
	}
}

func TestAssistGeneratorMapsRequestAndAppliesPanelDefaults(t *testing.T) {
	var captured AssistInput
	flow := New(func(_ context.Context, input AssistInput) (AssistOutput, error) {
		captured = input
		return AssistOutput{Text: "Answer", SpeakText: "Short answer", Action: "respond", Locale: "de"}, nil
	})

	result, err := AssistGenerator(flow).GenerateAssist(context.Background(), speechkit.AssistRequest{
		Text:      "erklaer mir das",
		Locale:    "de",
		Selection: "selected text",
		Context:   "Active application: Code",
	})
	if err != nil {
		t.Fatalf("GenerateAssist: %v", err)
	}
	if captured.Utterance != "erklaer mir das" || captured.Locale != "de" || captured.Selection != "selected text" || captured.Context != "Active application: Code" {
		t.Fatalf("flow input = %#v, want the request fields forwarded", captured)
	}
	if result.Text != "Answer" || result.SpeakText != "Short answer" || result.Action != "respond" || result.Locale != "de" {
		t.Fatalf("result = %#v, want the flow output mapped", result)
	}
	if result.Surface != speechkit.AssistSurfacePanel || result.Kind != assist.KindAnswer {
		t.Fatalf("result surface/kind = %q/%q, want the LLM-path panel/answer defaults", result.Surface, result.Kind)
	}
}

func TestAssistGeneratorWrapsFlowErrors(t *testing.T) {
	flowErr := errors.New("model down: invalid configuration")
	flow := New(func(context.Context, AssistInput) (AssistOutput, error) {
		return AssistOutput{}, flowErr
	})

	_, err := AssistGenerator(flow).GenerateAssist(context.Background(), speechkit.AssistRequest{Text: "hi"})
	if !errors.Is(err, flowErr) {
		t.Fatalf("error = %v, want it to wrap %v", err, flowErr)
	}
	if !strings.HasPrefix(err.Error(), "assist: LLM failed: ") || !strings.Contains(err.Error(), "invalid configuration") {
		t.Fatalf("error = %q, want the LLM-failed prefix with the provider message", err)
	}
}
