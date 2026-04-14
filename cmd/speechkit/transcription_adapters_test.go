package main

import (
	"context"
	"strings"
	"testing"

	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/genkit"

	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/output"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type fakeTranscriptInterceptor struct {
	calls   int
	handled bool
	err     error
}

func (f *fakeTranscriptInterceptor) Intercept(_ context.Context, _ speechkit.Transcript, _ any) (bool, error) {
	f.calls++
	return f.handled, f.err
}

type fakeOutputHandler struct {
	calls  int
	result *stt.Result
}

func (f *fakeOutputHandler) Handle(_ context.Context, result *stt.Result, _ output.Target) error {
	f.calls++
	if result != nil {
		clone := *result
		f.result = &clone
	}
	return nil
}

func fixedAssistFlow(t *testing.T, output flows.AssistOutput) *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}] {
	t.Helper()

	g := genkit.Init(context.Background())
	name := "test_assist_" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	return genkit.DefineFlow(g, name, func(context.Context, flows.AssistInput) (flows.AssistOutput, error) {
		return output, nil
	})
}

func TestDesktopTranscriptOutput_AssistBypassesGlobalInterceptor(t *testing.T) {
	interceptor := &fakeTranscriptInterceptor{handled: true}
	handler := &fakeOutputHandler{}
	flow := fixedAssistFlow(t, flows.AssistOutput{
		Text:      "Assist reply",
		SpeakText: "Assist reply",
		Action:    "respond",
		Locale:    "de",
	})

	state := &appState{
		assistPipeline: assist.NewPipeline(flow, nil, nil, false),
	}

	var assistText string
	outputAdapter := desktopTranscriptOutput{
		state:       state,
		handler:     handler,
		interceptor: interceptor,
		activeMode: func() string {
			return "agent"
		},
		agentMode: func() string {
			return "assist"
		},
		onAssistText: func(text string) {
			assistText = text
		},
	}

	err := outputAdapter.Deliver(context.Background(), speechkit.Transcript{
		Text:     "erklaer mir kurz die aenderung",
		Language: "de",
	}, output.Target{})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if interceptor.calls != 0 {
		t.Fatalf("global interceptor calls = %d, want 0 in assist mode", interceptor.calls)
	}
	if got, want := assistText, "Assist reply"; got != want {
		t.Fatalf("assist bubble text = %q, want %q", got, want)
	}
	if handler.calls != 1 {
		t.Fatalf("output handler calls = %d, want 1", handler.calls)
	}
	if handler.result == nil || handler.result.Text != "Assist reply" {
		t.Fatalf("output text = %q, want assist reply", handler.result.Text)
	}
}

func TestDesktopTranscriptOutput_DictateStillUsesGlobalInterceptor(t *testing.T) {
	interceptor := &fakeTranscriptInterceptor{handled: true}
	handler := &fakeOutputHandler{}

	outputAdapter := desktopTranscriptOutput{
		handler:     handler,
		interceptor: interceptor,
		activeMode: func() string {
			return "dictate"
		},
	}

	err := outputAdapter.Deliver(context.Background(), speechkit.Transcript{
		Text:     "summarize this",
		Language: "en",
	}, output.Target{})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if interceptor.calls != 1 {
		t.Fatalf("global interceptor calls = %d, want 1 in dictate mode", interceptor.calls)
	}
	if handler.calls != 0 {
		t.Fatalf("output handler calls = %d, want 0 when interceptor handles dictate transcript", handler.calls)
	}
}
