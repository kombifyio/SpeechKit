package assist

import (
	"context"
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	ttspkg "github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

func TestServiceProcessesMatchedTool(t *testing.T) {
	service, err := NewService(Options{
		Behavior: speechkit.ModeBehaviorClean,
		Matcher: ToolMatcherFunc(func(context.Context, speechkit.AssistRequest) (ToolCall, bool, error) {
			return ToolCall{Intent: "copy_last", Locale: "en"}, true, nil
		}),
		Executor: ToolExecutorFunc(func(context.Context, ToolCall) (ToolResult, error) {
			return ToolResult{Text: "Copied", Action: "execute"}, nil
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

type fakeTTSRouter struct {
	text string
	opts ttspkg.SynthesizeOpts
}

func (r *fakeTTSRouter) Synthesize(_ context.Context, text string, opts ttspkg.SynthesizeOpts) (*ttspkg.Result, error) {
	r.text = text
	r.opts = opts
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
