package assist_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
)

// ExampleService_Process shows the Assist boundary: a deterministic utility
// (the ToolMatcher/ToolExecutor pair) handles what it recognises and the
// Generator — normally an LLM — takes everything else. The result is one-shot
// text; the host decides where it goes.
func ExampleService_Process() {
	svc, err := assist.NewService(assist.Options{
		Matcher: assist.ToolMatcherFunc(func(_ context.Context, req speechkit.AssistRequest) (assist.ToolCall, bool, error) {
			if strings.HasPrefix(req.Text, "uppercase ") {
				return assist.ToolCall{Intent: "uppercase", Payload: strings.TrimPrefix(req.Text, "uppercase ")}, true, nil
			}
			return assist.ToolCall{}, false, nil
		}),
		Executor: assist.ToolExecutorFunc(func(_ context.Context, call assist.ToolCall) (assist.ToolResult, error) {
			return assist.ToolResult{Text: strings.ToUpper(call.Payload), Surface: speechkit.AssistSurfaceReplace}, nil
		}),
		Generator: assist.GenerateFunc(func(_ context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
			return speechkit.AssistResult{Text: "llm: " + req.Text, Surface: speechkit.AssistSurfacePanel}, nil
		}),
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	res, _ := svc.Process(ctx, speechkit.AssistRequest{Text: "uppercase make this loud", Locale: "en"})
	fmt.Println(res.Text, res.Surface, res.ShortcutID)

	res, _ = svc.Process(ctx, speechkit.AssistRequest{Text: "summarise the meeting", Locale: "en"})
	fmt.Println(res.Text, res.Surface)
	// Output:
	// MAKE THIS LOUD replace uppercase
	// llm: summarise the meeting panel
}

// ExampleService_Process_cleanMode restricts Assist to deterministic utilities
// only: no request ever reaches an LLM. Hosts use this for privacy-sensitive
// deployments and test the branch with errors.Is.
func ExampleService_Process_cleanMode() {
	svc, err := assist.NewService(assist.Options{
		Behavior: speechkit.ModeBehaviorClean,
		Executor: assist.ToolExecutorFunc(func(context.Context, assist.ToolCall) (assist.ToolResult, error) {
			return assist.ToolResult{}, nil
		}),
	})
	if err != nil {
		log.Fatal(err)
	}

	_, err = svc.Process(context.Background(), speechkit.AssistRequest{Text: "write me a poem"})
	fmt.Println(errors.Is(err, assist.ErrCleanModeNeedsUtility))
	// Output: true
}
