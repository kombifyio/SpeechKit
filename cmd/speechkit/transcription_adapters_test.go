package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/genkit"

	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/config"
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

func fixedAssistFlow(t *testing.T, assistOutput flows.AssistOutput) *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}] {
	t.Helper()

	g := genkit.Init(context.Background())
	name := "test_assist_" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	return genkit.DefineFlow(g, name, func(context.Context, flows.AssistInput) (flows.AssistOutput, error) {
		return assistOutput, nil
	})
}

func failingAssistFlow(t *testing.T, flowErr error) *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}] {
	t.Helper()

	g := genkit.Init(context.Background())
	name := "test_assist_failure_" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	return genkit.DefineFlow(g, name, func(context.Context, flows.AssistInput) (flows.AssistOutput, error) {
		return flows.AssistOutput{}, flowErr
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

	prompter := &fakeOverlayWindow{}
	state := &appState{
		assistPipeline: assist.NewPipeline(flow, nil, nil, false),
		prompterWindow: prompter,
	}

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
	if handler.calls != 0 {
		t.Fatalf("output handler calls = %d, want 0 for assist side-panel delivery", handler.calls)
	}
	combinedScripts := strings.Join(prompter.scripts, "\n")
	if !strings.Contains(combinedScripts, `setMode("assist")`) {
		t.Fatalf("prompter scripts missing assist mode switch: %s", combinedScripts)
	}
	if !strings.Contains(combinedScripts, `role:"user",text:"erklaer mir kurz die aenderung",done:true`) {
		t.Fatalf("prompter scripts missing user transcript: %s", combinedScripts)
	}
	if !strings.Contains(combinedScripts, `role:"assistant",text:"Assist reply",done:true`) {
		t.Fatalf("prompter scripts missing assistant response: %s", combinedScripts)
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

func TestDesktopTranscriptOutput_AssistExplainsQuotaFailureAndSuggestsFallback(t *testing.T) {
	interceptor := &fakeTranscriptInterceptor{}
	handler := &fakeOutputHandler{}
	flow := failingAssistFlow(t, fmt.Errorf(`assist: all models failed: gpt-5.4-2026-03-05 error (429): { "error": { "message": "You exceeded your current quota, please check your plan and billing details.", "type": "insufficient_quota", "param": null, "code": "insufficient_quota" } }`))

	prompter := &fakeOverlayWindow{}
	state := &appState{
		assistPipeline: assist.NewPipeline(flow, nil, nil, false),
		prompterWindow: prompter,
	}

	outputAdapter := desktopTranscriptOutput{
		cfg: &config.Config{
			ModelSelection: config.ModelSelectionConfig{
				Assist: config.ModeModelSelection{
					PrimaryProfileID: "assist.openai.gpt-5.4",
				},
			},
		},
		state:       state,
		handler:     handler,
		interceptor: interceptor,
		activeMode: func() string {
			return modeAssist
		},
	}

	err := outputAdapter.Deliver(context.Background(), speechkit.Transcript{
		Text:     "antworte bitte kurz",
		Language: "de",
	}, output.Target{})
	if err == nil {
		t.Fatal("Deliver() error = nil, want assist failure")
	}

	combinedScripts := strings.Join(prompter.scripts, "\n")
	if !strings.Contains(combinedScripts, `provider quota is exhausted`) {
		t.Fatalf("prompter scripts missing quota explanation: %s", combinedScripts)
	}
	if !strings.Contains(combinedScripts, `Configure a fallback model in Settings`) {
		t.Fatalf("prompter scripts missing fallback guidance: %s", combinedScripts)
	}
}
