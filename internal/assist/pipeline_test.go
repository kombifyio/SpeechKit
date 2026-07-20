package assist

import (
	"context"
	"testing"

	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/genkit"

	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
	"github.com/kombifyio/SpeechKit/internal/tts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
)

type mockTTSProvider struct {
	audio []byte
	err   error
	opts  tts.SynthesizeOpts
}

func (m *mockTTSProvider) Synthesize(_ context.Context, text string, opts tts.SynthesizeOpts) (*tts.Result, error) {
	m.opts = opts
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
func (m *mockTTSProvider) Kind() tts.ProviderKind         { return tts.ProviderKindDirectProvider }
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

func TestSmartHomeIntentNeverFallsThroughToLLMWhenHAIsUnavailable(t *testing.T) {
	tests := []struct {
		name       string
		transcript string
		locale     string
		wantLocale string
	}{
		{name: "English", transcript: "turn on the kitchen light", locale: "en-US", wantLocale: "en"},
		{name: "German", transcript: "schalte das Küchenlicht ein", locale: "de-DE", wantLocale: "de"},
		{name: "Spanish", transcript: "enciende la luz de la cocina", locale: "es-MX", wantLocale: "es"},
		{name: "Simplified Chinese", transcript: "家庭助理打开客厅灯", locale: "zh-Hans-CN", wantLocale: "zh-Hans"},
		{name: "Hindi", transcript: "लाइट चालू करो", locale: "hi-IN", wantLocale: "hi"},
		{name: "Arabic", transcript: "شغّل الضوء في المطبخ", locale: "ar-EG", wantLocale: "ar"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockTTS := &mockTTSProvider{audio: []byte("failure-audio")}
			pipeline := NewPipeline(fixedAssistFlow(t, flows.AssistOutput{
				Text:      "unsafe general model response",
				SpeakText: "unsafe general model response",
				Action:    "respond",
				Locale:    "en",
			}), nil, tts.NewRouter(tts.StrategyCloudFirst, mockTTS), true)

			result, err := pipeline.Process(context.Background(), tc.transcript, ProcessOpts{Locale: tc.locale})
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if result.Text == "unsafe general model response" {
				t.Fatal("recognized smart-home request reached the general Assist model")
			}
			if result.Action == "silent" || result.Text == "" {
				t.Fatalf("smart-home denial must be terminal: %#v", result)
			}
			if got, want := result.Shortcut, string(shortcuts.IntentHomeAssistant); got != want {
				t.Fatalf("Shortcut = %q, want %q", got, want)
			}
			if result.MessageID != localization.CompanionHomeAssistantNotConfigured || result.ReasonCode != "not_configured" {
				t.Fatalf("smart-home denial metadata = %q/%q", result.MessageID, result.ReasonCode)
			}
			if result.Locale != tc.wantLocale || mockTTS.opts.Locale != tc.wantLocale {
				t.Fatalf("result/TTS locale = %q/%q, want %q", result.Locale, mockTTS.opts.Locale, tc.wantLocale)
			}
			if want := localization.Text(tc.wantLocale, result.MessageID); result.Text != want {
				t.Fatalf("result text = %q, want %q", result.Text, want)
			}
		})
	}
}

// TestSilentToolResultFallsThroughToLLM covers the Voice-Companion
// fallthrough contract: when a registered skill matches an intent but
// returns Action="silent" + empty Text (the documented "I can't answer
// this specific payload — defer to LLM" signal), the pipeline must
// re-route to handleLLM instead of returning silence. Documented in
// docs/voice-companion.md and used by MathSkill on unparseable
// expressions and WikipediaSkill on disambiguation. Home Assistant never
// emits a silent result because smart-home requests are fail-closed.
func TestSilentToolResultFallsThroughToLLM(t *testing.T) {
	executor := &mockToolExecutor{
		result: ToolResult{
			Action: "silent",
			Locale: "en",
		},
	}
	pipeline := NewPipeline(fixedAssistFlow(t, flows.AssistOutput{
		Text:      "LLM picked it up",
		SpeakText: "LLM picked it up",
		Action:    "respond",
		Locale:    "en",
	}), executor, nil, false)

	// "summarize this" is a registered shortcut intent — the resolver
	// will route to RouteToolIntent, the executor will return silent.
	result, err := pipeline.Process(context.Background(), "summarize this", ProcessOpts{Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executor.calls != 1 {
		t.Errorf("expected executor to be called once, got %d", executor.calls)
	}
	if result.Text != "LLM picked it up" {
		t.Errorf("expected LLM-text fallthrough, got Text=%q", result.Text)
	}
	if result.Action != "respond" {
		t.Errorf("expected Action=respond after fallthrough, got %q", result.Action)
	}
}

// TestSilentToolResultWithoutLLMReturnsSilent guards the inverse: when
// no LLM is configured, the silent skill response is honoured rather
// than dropped. This preserves the legacy QuickNote-style "silent"
// behaviour for hosts that intentionally have no LLM provider.
func TestSilentToolResultWithoutLLMReturnsSilent(t *testing.T) {
	executor := &mockToolExecutor{
		result: ToolResult{
			Action: "silent",
			Locale: "en",
		},
	}
	pipeline := NewPipeline(nil, executor, nil, false)
	result, err := pipeline.Process(context.Background(), "summarize this", ProcessOpts{Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "silent" {
		t.Errorf("expected Action=silent without LLM, got %q", result.Action)
	}
}

// TestSilentToolResultWithTextStaysSilent covers the case where a host
// returns Action="silent" with explanatory Text (e.g. "Already saved
// silently"). That should NOT trigger LLM fallthrough — the host is
// explicitly choosing silence with a known acknowledgement.
func TestSilentToolResultWithTextStaysSilent(t *testing.T) {
	executor := &mockToolExecutor{
		result: ToolResult{
			Action: "silent",
			Text:   "Already saved silently",
			Locale: "en",
		},
	}
	pipeline := NewPipeline(fixedAssistFlow(t, flows.AssistOutput{
		Text: "LLM fallback",
	}), executor, nil, false)

	result, err := pipeline.Process(context.Background(), "summarize this", ProcessOpts{Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "Already saved silently" {
		t.Errorf("expected host-provided silent Text, got %q", result.Text)
	}
}

func TestCanHandleWithoutDirectReplyModelRejectsModelRequiredUtilities(t *testing.T) {
	pipeline := NewPipeline(nil, &mockToolExecutor{}, nil, false)

	if pipeline.CanHandleWithoutDirectReplyModel("summarize this", ProcessOpts{Locale: "en"}) {
		t.Fatal("CanHandleWithoutDirectReplyModel returned true for model-required summarize utility")
	}
	if !pipeline.CanHandleWithoutDirectReplyModel("copy last", ProcessOpts{Locale: "en"}) {
		t.Fatal("CanHandleWithoutDirectReplyModel returned false for action-only copy utility")
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
