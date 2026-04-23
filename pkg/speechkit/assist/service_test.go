package assist

import (
	"context"
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
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
