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

type fakeAssistExecutor struct {
	calls  int
	call   assist.ToolCall
	result assist.ToolResult
	err    error
}

func (f *fakeAssistExecutor) Execute(_ context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	f.calls++
	f.call = call
	return f.result, f.err
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

func fixedAgentFlow(t *testing.T, text string) *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}] {
	t.Helper()

	g := genkit.Init(context.Background())
	name := "test_agent_" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	return genkit.DefineFlow(g, name, func(context.Context, flows.AgentInput) (flows.AgentOutput, error) {
		return flows.AgentOutput{Text: text, Action: "display"}, nil
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

func TestDesktopTranscriptOutput_DictateBypassesGlobalInterceptor(t *testing.T) {
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
	if interceptor.calls != 0 {
		t.Fatalf("global interceptor calls = %d, want 0 in dictate mode", interceptor.calls)
	}
	if handler.calls != 1 {
		t.Fatalf("output handler calls = %d, want 1 for dictate passthrough", handler.calls)
	}
	if handler.result == nil || handler.result.Text != "summarize this" {
		t.Fatalf("output handler result = %#v, want dictate transcript passthrough", handler.result)
	}
}

func TestDesktopTranscriptOutput_VoiceAgentUsesBrainstormingAgentFlow(t *testing.T) {
	interceptor := &fakeTranscriptInterceptor{handled: true}
	handler := &fakeOutputHandler{}
	flow := fixedAssistFlow(t, flows.AssistOutput{
		Text:      "Assist reply",
		SpeakText: "Assist reply",
		Action:    "respond",
		Locale:    "en",
	})

	prompter := &fakeOverlayWindow{}
	state := &appState{
		assistPipeline: assist.NewPipeline(flow, nil, nil, false),
		agentFlow:      fixedAgentFlow(t, "Agent brainstorm reply"),
		prompterWindow: prompter,
	}

	outputAdapter := desktopTranscriptOutput{
		state:       state,
		handler:     handler,
		interceptor: interceptor,
		activeMode: func() string {
			return modeVoiceAgent
		},
	}

	err := outputAdapter.Deliver(context.Background(), speechkit.Transcript{
		Text:     "brainstorm mit mir die naechsten schritte",
		Language: "de",
	}, output.Target{})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if interceptor.calls != 0 {
		t.Fatalf("global interceptor calls = %d, want 0 in voice agent mode", interceptor.calls)
	}
	if handler.calls != 0 {
		t.Fatalf("output handler calls = %d, want 0 in voice agent mode", handler.calls)
	}

	combinedScripts := strings.Join(prompter.scripts, "\n")
	if !strings.Contains(combinedScripts, `setMode("voice_agent")`) {
		t.Fatalf("prompter scripts missing voice agent mode switch: %s", combinedScripts)
	}
	if !strings.Contains(combinedScripts, `Agent brainstorm reply`) {
		t.Fatalf("prompter scripts missing voice agent response: %s", combinedScripts)
	}
	if strings.Contains(combinedScripts, `Assist reply`) {
		t.Fatalf("prompter scripts leaked assist utility response: %s", combinedScripts)
	}
	sessionTranscript := state.voiceAgentSessionTranscript()
	if !strings.Contains(sessionTranscript, "User: brainstorm mit mir die naechsten schritte") {
		t.Fatalf("voice agent transcript missing user turn: %s", sessionTranscript)
	}
	if !strings.Contains(sessionTranscript, "Assistant: Agent brainstorm reply") {
		t.Fatalf("voice agent transcript missing assistant turn: %s", sessionTranscript)
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

func TestDesktopTranscriptOutput_AssistExplainsUnsupportedHuggingFaceModel(t *testing.T) {
	interceptor := &fakeTranscriptInterceptor{}
	handler := &fakeOutputHandler{}
	flow := failingAssistFlow(t, fmt.Errorf(`assist: LLM failed: assist: all models failed: Qwen/Qwen3.5-27B error (400): {"error":"Model not supported by provider hf-inference"}`))

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
	if strings.Contains(combinedScripts, `Conversation failed. Check Settings and try again.`) {
		t.Fatalf("prompter scripts still use generic failure: %s", combinedScripts)
	}
	if !strings.Contains(combinedScripts, `Qwen/Qwen3.5-27B`) {
		t.Fatalf("prompter scripts missing failing model name: %s", combinedScripts)
	}
	if !strings.Contains(combinedScripts, `not supported by Hugging Face Inference`) {
		t.Fatalf("prompter scripts missing provider support explanation: %s", combinedScripts)
	}
	if !strings.Contains(combinedScripts, `Settings`) || !strings.Contains(combinedScripts, `Models`) {
		t.Fatalf("prompter scripts missing model settings guidance: %s", combinedScripts)
	}
}

func TestDesktopTranscriptOutput_AssistActionAckSkipsPrompterPanel(t *testing.T) {
	prompter := &fakeOverlayWindow{}
	executor := &fakeAssistExecutor{
		result: assist.ToolResult{
			Text:      "Copied to clipboard.",
			SpeakText: "Copied to clipboard.",
			Action:    "execute",
			Locale:    "en",
			Surface:   assist.ResultSurfaceActionAck,
			Kind:      assist.ResultKindUtilityAction,
		},
	}

	var acknowledged string
	state := &appState{
		assistPipeline: assist.NewPipeline(nil, executor, nil, false),
		prompterWindow: prompter,
	}

	outputAdapter := desktopTranscriptOutput{
		state: state,
		activeMode: func() string {
			return modeAssist
		},
		onAssistText: func(text string) {
			acknowledged = text
		},
	}

	err := outputAdapter.Deliver(context.Background(), speechkit.Transcript{
		Text:     "copy last",
		Language: "en",
	}, output.Target{})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	if acknowledged != "" {
		t.Fatalf("acknowledged text = %q, want no bubble for utility acknowledgement", acknowledged)
	}
	if len(prompter.scripts) != 0 {
		t.Fatalf("prompter scripts = %v, want no assist panel activity for action acknowledgement", prompter.scripts)
	}
}

func TestDesktopTranscriptOutput_AssistDirectReplyWithoutModelShowsBubbleOnly(t *testing.T) {
	prompter := &fakeOverlayWindow{}
	bubble := &fakeOverlayWindow{}
	executor := &fakeAssistExecutor{}
	state := &appState{
		assistPipeline:    assist.NewPipeline(nil, executor, nil, false),
		prompterWindow:    prompter,
		assistBubble:      bubble,
		assistOverlayMode: config.OverlayFeedbackModeBigProductivity,
	}

	outputAdapter := desktopTranscriptOutput{
		state: state,
		activeMode: func() string {
			return modeAssist
		},
	}

	err := outputAdapter.Deliver(context.Background(), speechkit.Transcript{
		Text:     "erklaer mir bitte die aktuelle auswahl",
		Language: "de",
	}, output.Target{})
	if err != nil {
		t.Fatalf("Deliver() error = %v, want nil for missing Assist model guidance", err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0 for direct reply without model", executor.calls)
	}
	if prompter.showCalls != 0 || len(prompter.scripts) != 0 {
		t.Fatalf("prompter show calls = %d scripts = %v, want no persistent Assist panel", prompter.showCalls, prompter.scripts)
	}
	if bubble.showCalls != 1 {
		t.Fatalf("assist bubble show calls = %d, want 1", bubble.showCalls)
	}
	if len(bubble.scripts) == 0 || !strings.Contains(strings.Join(bubble.scripts, "\n"), "Assist model") {
		t.Fatalf("assist bubble scripts = %v, want model guidance", bubble.scripts)
	}
}

func TestDesktopTranscriptOutput_AssistDirectReplyWithoutModelUsesSmallFeedback(t *testing.T) {
	prompter := &fakeOverlayWindow{}
	bubble := &fakeOverlayWindow{}
	executor := &fakeAssistExecutor{}
	state := &appState{
		assistPipeline:    assist.NewPipeline(nil, executor, nil, false),
		prompterWindow:    prompter,
		assistBubble:      bubble,
		assistOverlayMode: config.OverlayFeedbackModeSmallFeedback,
	}

	outputAdapter := desktopTranscriptOutput{
		state: state,
		activeMode: func() string {
			return modeAssist
		},
	}

	err := outputAdapter.Deliver(context.Background(), speechkit.Transcript{
		Text:     "erklaer mir bitte die aktuelle auswahl",
		Language: "de",
	}, output.Target{})
	if err != nil {
		t.Fatalf("Deliver() error = %v, want nil for missing Assist model guidance", err)
	}
	if bubble.showCalls != 0 {
		t.Fatalf("assist bubble show calls = %d, want 0 in small feedback mode", bubble.showCalls)
	}
	if got := state.overlaySnapshot().Text; !strings.Contains(got, "Assist model") {
		t.Fatalf("overlay feedback text = %q, want model guidance", got)
	}
}

func TestDesktopTranscriptOutput_AssistEmptyTranscriptDoesNotOpenPrompter(t *testing.T) {
	prompter := &fakeOverlayWindow{}
	state := &appState{
		assistPipeline: assist.NewPipeline(nil, nil, nil, false),
		prompterWindow: prompter,
	}

	outputAdapter := desktopTranscriptOutput{
		state: state,
		activeMode: func() string {
			return modeAssist
		},
		onAssistText: func(text string) {
			t.Fatalf("onAssistText called for empty transcript: %q", text)
		},
	}

	err := outputAdapter.Deliver(context.Background(), speechkit.Transcript{
		Text:     "   ",
		Language: "de",
	}, output.Target{})
	if err != nil {
		t.Fatalf("Deliver() error = %v, want nil for empty assist transcript", err)
	}
	if len(prompter.scripts) != 0 {
		t.Fatalf("prompter scripts = %v, want no assist panel activity for empty transcript", prompter.scripts)
	}
	if prompter.showCalls != 0 {
		t.Fatalf("prompter show calls = %d, want 0 for empty transcript", prompter.showCalls)
	}
}
