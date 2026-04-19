package assist

import (
	"context"
	"testing"

	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/genkit"

	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
	"github.com/kombifyio/SpeechKit/internal/tts"
)

type mockTTSProvider struct {
	audio []byte
	err   error
}

func (m *mockTTSProvider) Synthesize(_ context.Context, text string, _ tts.SynthesizeOpts) (*tts.Result, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &tts.Result{
		Audio:    m.audio,
		Format:   "mp3",
		Provider: "mock",
	}, nil
}

func (m *mockTTSProvider) Name() string                   { return "mock" }
func (m *mockTTSProvider) Health(_ context.Context) error { return nil }

type mockToolExecutor struct {
	calls  int
	call   ToolCall
	result ToolResult
	err    error
}

func (m *mockToolExecutor) Execute(_ context.Context, call ToolCall) (ToolResult, error) {
	m.calls++
	m.call = call
	return m.result, m.err
}

func fixedAssistFlow(t *testing.T, output flows.AssistOutput) *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}] {
	t.Helper()

	g := genkit.Init(context.Background())
	return genkit.DefineFlow(g, "test_assist_"+t.Name(), func(context.Context, flows.AssistInput) (flows.AssistOutput, error) {
		return output, nil
	})
}

func TestProcessShortcut(t *testing.T) {
	mockTTS := &mockTTSProvider{audio: []byte("fake-audio")}
	router := tts.NewRouter(tts.StrategyCloudFirst, mockTTS)
	executor := &mockToolExecutor{
		result: ToolResult{
			Text:      "Copied to clipboard.",
			SpeakText: "Copied to clipboard.",
			Action:    "execute",
			Locale:    "en",
			Surface:   ResultSurfaceActionAck,
			Kind:      ResultKindUtilityAction,
		},
	}
	pipeline := NewPipeline(nil, executor, router, true)

	result, err := pipeline.Process(context.Background(), "copy last", ProcessOpts{Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "execute" {
		t.Errorf("expected action 'execute', got %s", result.Action)
	}
	if result.Surface != ResultSurfaceActionAck {
		t.Errorf("expected action ack surface, got %s", result.Surface)
	}
	if result.Kind != ResultKindUtilityAction {
		t.Errorf("expected utility result kind, got %s", result.Kind)
	}
	if result.Shortcut != "copy_last" {
		t.Errorf("expected shortcut 'copy_last', got %s", result.Shortcut)
	}
	if result.Text == "" {
		t.Error("expected non-empty text")
	}
	if len(result.Audio) == 0 {
		t.Error("expected audio when TTS enabled")
	}
}

func TestProcessShortcutGerman(t *testing.T) {
	mockTTS := &mockTTSProvider{audio: []byte("audio")}
	router := tts.NewRouter(tts.StrategyCloudFirst, mockTTS)
	executor := &mockToolExecutor{
		result: ToolResult{
			Text:      "Wird zusammengefasst...",
			SpeakText: "Wird zusammengefasst...",
			Action:    "execute",
			Locale:    "de",
			Surface:   ResultSurfaceActionAck,
			Kind:      ResultKindUtilityAction,
		},
	}
	pipeline := NewPipeline(nil, executor, router, true)

	result, err := pipeline.Process(context.Background(), "zusammenfassen", ProcessOpts{Locale: "de"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Shortcut != "summarize" {
		t.Errorf("expected shortcut 'summarize', got %s", result.Shortcut)
	}
	if result.Text != "Wird zusammengefasst..." {
		t.Errorf("unexpected text: %s", result.Text)
	}
}

func TestProcessNoTTS(t *testing.T) {
	executor := &mockToolExecutor{
		result: ToolResult{
			Text:      "Copied to clipboard.",
			SpeakText: "Copied to clipboard.",
			Action:    "execute",
			Locale:    "en",
			Surface:   ResultSurfaceActionAck,
			Kind:      ResultKindUtilityAction,
		},
	}
	pipeline := NewPipeline(nil, executor, nil, false)

	result, err := pipeline.Process(context.Background(), "copy last", ProcessOpts{Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text == "" {
		t.Error("expected text even without TTS")
	}
	if len(result.Audio) != 0 {
		t.Error("expected no audio when TTS disabled")
	}
}

func TestProcessEmptyTranscript(t *testing.T) {
	pipeline := NewPipeline(nil, nil, nil, false)
	_, err := pipeline.Process(context.Background(), "", ProcessOpts{})
	if err == nil {
		t.Fatal("expected error for empty transcript")
	}
}

func TestProcessNoLLMFallsThrough(t *testing.T) {
	pipeline := NewPipeline(nil, nil, nil, false)

	// Non-shortcut text with no LLM configured.
	_, err := pipeline.Process(context.Background(), "what is the weather today", ProcessOpts{Locale: "en"})
	if err == nil {
		t.Fatal("expected error when no LLM configured")
	}
}

func TestProcessDirectReplySkipsToolExecution(t *testing.T) {
	executor := &mockToolExecutor{}
	pipeline := NewPipeline(fixedAssistFlow(t, flows.AssistOutput{
		Text:      "Direkte Antwort",
		SpeakText: "Direkte Antwort",
		Action:    "respond",
		Locale:    "de",
	}), executor, nil, false)

	result, err := pipeline.Process(context.Background(), "erklaer mir den unterschied", ProcessOpts{Locale: "de"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0 for direct reply", executor.calls)
	}
	if got, want := result.Text, "Direkte Antwort"; got != want {
		t.Fatalf("result.Text = %q, want %q", got, want)
	}
	if got, want := result.Action, "respond"; got != want {
		t.Fatalf("result.Action = %q, want %q", got, want)
	}
	if got, want := result.Surface, ResultSurfacePanel; got != want {
		t.Fatalf("result.Surface = %q, want %q", got, want)
	}
	if got, want := result.Kind, ResultKindAnswer; got != want {
		t.Fatalf("result.Kind = %q, want %q", got, want)
	}
}

func TestProcessCommandPrefixCallsToolExecutor(t *testing.T) {
	executor := &mockToolExecutor{
		result: ToolResult{
			Text:      "Kurzfassung",
			SpeakText: "Kurzfassung",
			Action:    "execute",
			Locale:    "de",
			Surface:   ResultSurfacePanel,
			Kind:      ResultKindWorkProduct,
		},
	}
	pipeline := NewPipeline(fixedAssistFlow(t, flows.AssistOutput{
		Text:      "sollte nicht verwendet werden",
		SpeakText: "sollte nicht verwendet werden",
		Action:    "respond",
		Locale:    "de",
	}), executor, nil, false)

	result, err := pipeline.Process(context.Background(), "zusammenfassen in drei punkten", ProcessOpts{
		Locale:    "de",
		Selection: "Der markierte Text",
		Target:    "target-window",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	if got, want := executor.call.Intent, shortcuts.IntentSummarize; got != want {
		t.Fatalf("executor intent = %q, want %q", got, want)
	}
	if got, want := executor.call.Payload, "in drei punkten"; got != want {
		t.Fatalf("executor payload = %q, want %q", got, want)
	}
	if got, want := executor.call.Selection, "Der markierte Text"; got != want {
		t.Fatalf("executor selection = %q, want %q", got, want)
	}
	if got, want := executor.call.Target, "target-window"; got != want {
		t.Fatalf("executor target = %#v, want %#v", got, want)
	}
	if got, want := result.Text, "Kurzfassung"; got != want {
		t.Fatalf("result.Text = %q, want %q", got, want)
	}
	if got, want := result.Action, "execute"; got != want {
		t.Fatalf("result.Action = %q, want %q", got, want)
	}
	if got, want := result.Surface, ResultSurfacePanel; got != want {
		t.Fatalf("result.Surface = %q, want %q", got, want)
	}
	if got, want := result.Kind, ResultKindWorkProduct; got != want {
		t.Fatalf("result.Kind = %q, want %q", got, want)
	}
}

func TestProcessCommandPrefixWithoutExecutorFails(t *testing.T) {
	pipeline := NewPipeline(nil, nil, nil, false)

	_, err := pipeline.Process(context.Background(), "copy last", ProcessOpts{Locale: "en"})
	if err == nil {
		t.Fatal("expected error when command prefix is detected without tool executor")
	}
}
