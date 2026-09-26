package assist

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	ttspkg "github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

func TestServiceProcessesMatchedTool(t *testing.T) {
	service, err := NewService(Options{
		Behavior: speechkit.ModeBehaviorClean,
		Matcher: ToolMatcherFunc(func(context.Context, speechkit.AssistRequest) (ToolCall, bool, error) {
			return ToolCall{Intent: "copy_last", Locale: "en"}, true, nil
		}),
		Executor: ToolExecutorFunc(func(context.Context, ToolCall) (ToolResult, error) {
			return ToolResult{
				Text:       "Copied",
				Action:     "execute",
				MessageID:  localization.CompanionHomeAssistantUnavailable,
				ReasonCode: "unavailable",
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.Process(context.Background(), speechkit.AssistRequest{Text: "copy last"})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if got, want := result.Text, "Copied"; got != want {
		t.Fatalf("result text = %q, want %q", got, want)
	}
	if got, want := result.ShortcutID, "copy_last"; got != want {
		t.Fatalf("shortcut = %q, want %q", got, want)
	}
	if result.MessageID != localization.CompanionHomeAssistantUnavailable || result.ReasonCode != "unavailable" {
		t.Fatalf("result metadata = %q/%q", result.MessageID, result.ReasonCode)
	}
}

func TestServiceUsesGeneratorInIntelligenceMode(t *testing.T) {
	service, err := NewService(Options{
		Generator: GenerateFunc(func(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error) {
			return speechkit.AssistResult{Text: "Generated", Surface: speechkit.AssistSurfacePanel}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.Process(context.Background(), speechkit.AssistRequest{Text: "draft this"})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if got, want := result.Text, "Generated"; got != want {
		t.Fatalf("result text = %q, want %q", got, want)
	}
}

func TestServiceCleanModeRejectsUnmatchedLLM(t *testing.T) {
	service, err := NewService(Options{
		Behavior: speechkit.ModeBehaviorClean,
		Generator: GenerateFunc(func(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error) {
			return speechkit.AssistResult{Text: "Generated"}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = service.Process(context.Background(), speechkit.AssistRequest{Text: "draft this"})
	if !errors.Is(err, ErrCleanModeNeedsUtility) {
		t.Fatalf("Process() error = %v, want %v", err, ErrCleanModeNeedsUtility)
	}
}

func TestServiceRejectsEmptyText(t *testing.T) {
	service, err := NewService(Options{
		Generator: GenerateFunc(func(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error) {
			t.Fatal("generator must not run for empty text")
			return speechkit.AssistResult{}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	_, err = service.Process(context.Background(), speechkit.AssistRequest{Text: "   "})
	if !errors.Is(err, ErrEmptyText) {
		t.Fatalf("Process() error = %v, want %v", err, ErrEmptyText)
	}
}

func TestServiceReportsMissingGeneratorForUnmatchedText(t *testing.T) {
	service, err := NewService(Options{
		Matcher: ToolMatcherFunc(func(context.Context, speechkit.AssistRequest) (ToolCall, bool, error) {
			return ToolCall{}, false, nil
		}),
		Executor: ToolExecutorFunc(func(context.Context, ToolCall) (ToolResult, error) {
			return ToolResult{}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if service.HasGenerator() {
		t.Fatal("HasGenerator() = true without a generator")
	}
	_, err = service.Process(context.Background(), speechkit.AssistRequest{Text: "what is the weather today"})
	if !errors.Is(err, ErrMissingHandler) {
		t.Fatalf("Process() error = %v, want %v", err, ErrMissingHandler)
	}
}

func TestServiceHasGenerator(t *testing.T) {
	service, err := NewService(Options{
		Generator: GenerateFunc(func(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error) {
			return speechkit.AssistResult{Text: "ok"}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if !service.HasGenerator() {
		t.Fatal("HasGenerator() = false with a generator")
	}
	var nilService *Service
	if nilService.HasGenerator() {
		t.Fatal("nil service must report no generator")
	}
}

func TestServiceDefaultsToolResultSpeakTextActionAndSurface(t *testing.T) {
	service, err := NewService(Options{
		Matcher: ToolMatcherFunc(func(context.Context, speechkit.AssistRequest) (ToolCall, bool, error) {
			return ToolCall{Intent: "copy_last", Locale: "de"}, true, nil
		}),
		Executor: ToolExecutorFunc(func(context.Context, ToolCall) (ToolResult, error) {
			return ToolResult{Text: "Kopiert."}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, err := service.Process(context.Background(), speechkit.AssistRequest{Text: "copy last", Locale: "de"})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.SpeakText != "Kopiert." {
		t.Fatalf("SpeakText = %q, want the text as the spoken default", result.SpeakText)
	}
	if result.Action != "execute" {
		t.Fatalf("Action = %q, want execute default", result.Action)
	}
	if result.Surface != speechkit.AssistSurfaceActionAck {
		t.Fatalf("Surface = %q, want action_ack default", result.Surface)
	}
	if result.Locale != "de" {
		t.Fatalf("Locale = %q, want the call locale", result.Locale)
	}
}

func TestServiceWrapsExecutorErrorWithIntent(t *testing.T) {
	executorErr := errors.New("clipboard unavailable")
	service, err := NewService(Options{
		Matcher: ToolMatcherFunc(func(context.Context, speechkit.AssistRequest) (ToolCall, bool, error) {
			return ToolCall{Intent: "copy_last"}, true, nil
		}),
		Executor: ToolExecutorFunc(func(context.Context, ToolCall) (ToolResult, error) {
			return ToolResult{}, executorErr
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	_, err = service.Process(context.Background(), speechkit.AssistRequest{Text: "copy last"})
	if !errors.Is(err, executorErr) {
		t.Fatalf("Process() error = %v, want it to wrap %v", err, executorErr)
	}
	if !strings.Contains(err.Error(), `"copy_last"`) || !strings.Contains(err.Error(), "clipboard unavailable") {
		t.Fatalf("Process() error = %q, want the intent and the executor message", err)
	}
}

func TestServiceMatchedCallWithoutExecutorFails(t *testing.T) {
	service, err := NewService(Options{
		Matcher: ToolMatcherFunc(func(context.Context, speechkit.AssistRequest) (ToolCall, bool, error) {
			return ToolCall{Intent: "copy_last"}, true, nil
		}),
		Generator: GenerateFunc(func(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error) {
			return speechkit.AssistResult{Text: "must not run"}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	_, err = service.Process(context.Background(), speechkit.AssistRequest{Text: "copy last"})
	if !errors.Is(err, ErrMissingExecutor) {
		t.Fatalf("Process() error = %v, want %v", err, ErrMissingExecutor)
	}
}

func TestServiceSilentResultWithTextStaysSilent(t *testing.T) {
	generatorCalls := 0
	service, err := NewService(Options{
		Matcher: ToolMatcherFunc(func(context.Context, speechkit.AssistRequest) (ToolCall, bool, error) {
			return ToolCall{Intent: "quick_note", Locale: "en"}, true, nil
		}),
		Executor: ToolExecutorFunc(func(context.Context, ToolCall) (ToolResult, error) {
			return ToolResult{
				Text:    "Already saved silently",
				Action:  "silent",
				Surface: speechkit.AssistSurfaceSilent,
				Locale:  "en",
			}, nil
		}),
		Generator: GenerateFunc(func(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error) {
			generatorCalls++
			return speechkit.AssistResult{Text: "LLM fallback"}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, err := service.Process(context.Background(), speechkit.AssistRequest{Text: "note this"})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if generatorCalls != 0 {
		t.Fatalf("generator calls = %d, want 0 for a silent result that carries text", generatorCalls)
	}
	if result.Text != "Already saved silently" || result.Action != "silent" || result.Surface != speechkit.AssistSurfaceSilent {
		t.Fatalf("result = %#v, want the host-provided silent acknowledgement", result)
	}
}

func TestServiceSilentEmptyResultClearsSessionAndFallsThrough(t *testing.T) {
	store := NewInMemorySkillContextStore(time.Minute, nil)
	store.Set("user", "math", map[string]string{"awaiting": "1"})
	service, err := NewService(Options{
		SkillContexts: store,
		Executor: ToolExecutorFunc(func(context.Context, ToolCall) (ToolResult, error) {
			return ToolResult{Action: "silent", Surface: speechkit.AssistSurfaceSilent}, nil
		}),
		Generator: GenerateFunc(func(_ context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
			return speechkit.AssistResult{Text: "llm: " + req.Text, Surface: speechkit.AssistSurfacePanel}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, err := service.Process(context.Background(), speechkit.AssistRequest{Text: "what is love", SessionKey: "user"})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Text != "llm: what is love" {
		t.Fatalf("result text = %q, want the generator answer", result.Text)
	}
	if store.Len() != 0 {
		t.Fatalf("session store has %d entries, want the follow-up cleared before the generator ran", store.Len())
	}
}

func TestServiceFollowupForwardsTargetTranscriptAndState(t *testing.T) {
	store := NewInMemorySkillContextStore(time.Minute, nil)
	store.Set("user", "timer", map[string]string{"awaiting_duration": "1"})
	var got ToolCall
	service, err := NewService(Options{
		SkillContexts: store,
		Matcher: ToolMatcherFunc(func(context.Context, speechkit.AssistRequest) (ToolCall, bool, error) {
			t.Fatal("matcher must not run while a follow-up is active")
			return ToolCall{}, false, nil
		}),
		Executor: ToolExecutorFunc(func(_ context.Context, call ToolCall) (ToolResult, error) {
			got = call
			return ToolResult{Text: "Timer set for 5 minutes."}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	target := struct{ window string }{"editor"}
	result, err := service.Process(context.Background(), speechkit.AssistRequest{
		Text:       "5 minutes",
		Locale:     "en",
		Selection:  "selected",
		Context:    "Active application: Code",
		SessionKey: "user",
		Target:     target,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if got.Intent != "timer" || got.Payload != "5 minutes" || got.Transcript != "5 minutes" {
		t.Fatalf("follow-up call = %#v, want intent timer with the new turn as payload and transcript", got)
	}
	if got.Target != target || got.Selection != "selected" || got.Locale != "en" {
		t.Fatalf("follow-up call = %#v, want target, selection and locale forwarded", got)
	}
	if !strings.HasPrefix(got.Context, "Active application: Code\n--\n") || !strings.Contains(got.Context, "awaiting_duration=1") {
		t.Fatalf("follow-up context = %q, want the host context followed by the stored state", got.Context)
	}
	if result.ShortcutID != "timer" {
		t.Fatalf("ShortcutID = %q, want timer", result.ShortcutID)
	}
	if store.Len() != 0 {
		t.Fatalf("session store has %d entries, want cleared after a completed follow-up", store.Len())
	}
}

type fakeTTSRouter struct {
	text string
	opts ttspkg.SynthesizeOpts
	err  error
}

func (r *fakeTTSRouter) Synthesize(_ context.Context, text string, opts ttspkg.SynthesizeOpts) (*ttspkg.Result, error) {
	r.text = text
	r.opts = opts
	if r.err != nil {
		return nil, r.err
	}
	return &ttspkg.Result{Audio: []byte("pcm"), Format: "pcm16", Provider: "test"}, nil
}

func TestServiceSynthesizesSpeakTextWithRequestLocale(t *testing.T) {
	ttsRouter := &fakeTTSRouter{}
	service, err := NewService(Options{
		TTSEnabled: true,
		TTSRouter:  ttsRouter,
		Generator: GenerateFunc(func(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error) {
			return speechkit.AssistResult{
				Text:      "panel text",
				SpeakText: "spoken text",
				Locale:    "de-DE",
				Surface:   speechkit.AssistSurfacePanel,
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.Process(context.Background(), speechkit.AssistRequest{Text: "say this"})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if ttsRouter.text != "spoken text" {
		t.Fatalf("tts text = %q, want SpeakText", ttsRouter.text)
	}
	if ttsRouter.opts.Locale != "de-DE" {
		t.Fatalf("tts locale = %q, want de-DE", ttsRouter.opts.Locale)
	}
	if string(result.Audio.Bytes()) != "pcm" || result.Format != "pcm16" {
		t.Fatalf("result audio/format = %q/%q", result.Audio.Bytes(), result.Format)
	}
}

func TestServiceForwardsTTSOptionsAndIsolatesThem(t *testing.T) {
	ttsRouter := &fakeTTSRouter{}
	defaults := provideropts.Values{"speed": 1.2}
	overrides := map[string]provideropts.Values{"openai": {"voice": "alloy"}}
	service, err := NewService(Options{
		TTSEnabled: true,
		TTSRouter:  ttsRouter,
		TTS:        &TTSOptions{Defaults: defaults, ProviderOptions: overrides},
		Generator: GenerateFunc(func(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error) {
			return speechkit.AssistResult{Text: "spoken", Locale: "en", Surface: speechkit.AssistSurfacePanel}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	// Mutations after construction must not leak into synthesis.
	defaults["speed"] = 9.9
	overrides["openai"]["voice"] = "nova"

	if _, err := service.Process(context.Background(), speechkit.AssistRequest{Text: "say this"}); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if got := ttsRouter.opts.Options["speed"]; got != 1.2 {
		t.Fatalf("tts defaults speed = %v, want the cloned 1.2", got)
	}
	if got := ttsRouter.opts.ProviderOptionsByProvider["openai"]["voice"]; got != "alloy" {
		t.Fatalf("tts provider override voice = %v, want the cloned alloy", got)
	}
}

func TestServiceTTSFailureFailsUnlessBestEffort(t *testing.T) {
	ttsErr := errors.New("voice offline")
	generator := GenerateFunc(func(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error) {
		return speechkit.AssistResult{Text: "answer", Locale: "en", Surface: speechkit.AssistSurfacePanel}, nil
	})

	strict, err := NewService(Options{TTSEnabled: true, TTSRouter: &fakeTTSRouter{err: ttsErr}, Generator: generator})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if _, err := strict.Process(context.Background(), speechkit.AssistRequest{Text: "say this"}); !errors.Is(err, ttsErr) {
		t.Fatalf("strict Process() error = %v, want the TTS error", err)
	}

	lenient, err := NewService(Options{TTSEnabled: true, TTSRouter: &fakeTTSRouter{err: ttsErr}, TTSBestEffort: true, Generator: generator})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, err := lenient.Process(context.Background(), speechkit.AssistRequest{Text: "say this"})
	if err != nil {
		t.Fatalf("best-effort Process() error = %v, want the text result", err)
	}
	if result.Text != "answer" || result.Audio.Len() != 0 || result.Format != "" {
		t.Fatalf("best-effort result = %#v, want text without audio", result)
	}
}

func TestServiceDoesNotSynthesizeSilentResult(t *testing.T) {
	ttsRouter := &fakeTTSRouter{}
	service, err := NewService(Options{
		TTSEnabled: true,
		TTSRouter:  ttsRouter,
		Generator: GenerateFunc(func(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error) {
			return speechkit.AssistResult{
				Text:    "do not speak",
				Surface: speechkit.AssistSurfaceSilent,
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.Process(context.Background(), speechkit.AssistRequest{Text: "silent action"})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if ttsRouter.text != "" {
		t.Fatalf("tts should not be called for silent surface, got text %q", ttsRouter.text)
	}
	if len(result.Audio.Bytes()) != 0 {
		t.Fatalf("silent result should not have audio: %q", result.Audio.Bytes())
	}
}
