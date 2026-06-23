package main

import (
	"context"
	"fmt"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/companion"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
)

type echoAssist struct{}

func (echoAssist) Process(_ context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	return speechkit.AssistResult{
		Text:    "assist handled: " + req.Text,
		Surface: speechkit.AssistSurfacePanel,
		Locale:  req.Locale,
	}, nil
}

func main() {
	ctx := context.Background()
	runtime := speechkit.NewRuntime(speechkit.Snapshot{}, speechkit.Hooks{
		HandleCommand: func(_ context.Context, cmd speechkit.Command) error {
			if cmd.Type == speechkit.CommandStartMode {
				fmt.Printf("host command: %s target=%s\n", cmd.Type, cmd.Metadata["hands_free_target"])
			}
			return nil
		},
	})
	defer runtime.Close()

	assistCompanion, err := companion.NewHandsFree(companion.Options{
		Runtime:    runtime,
		TargetMode: companion.TargetAssist,
		Assist:     echoAssist{},
		WakeRequest: func(_ context.Context, ev wakeword.DetectionEvent) (speechkit.AssistRequest, bool) {
			return speechkit.AssistRequest{Text: ev.Phrase, Locale: "en-US"}, false
		},
	})
	if err != nil {
		panic(err)
	}
	if err := assistCompanion.Start(ctx); err != nil {
		panic(err)
	}
	if err := assistCompanion.HandleWake(ctx, wakeword.DetectionEvent{Phrase: "Hey Quby", Mode: "assist"}); err != nil {
		panic(err)
	}

	dictationCompanion, err := companion.NewHandsFree(companion.Options{
		Runtime:    runtime,
		TargetMode: companion.TargetDictationUIAssisted,
	})
	if err != nil {
		panic(err)
	}
	if err := dictationCompanion.HandleWake(ctx, wakeword.DetectionEvent{Phrase: "Hey Quby", Mode: "dictation_ui_assisted"}); err != nil {
		panic(err)
	}
	fmt.Println((<-assistCompanion.Events()).Type)
}
