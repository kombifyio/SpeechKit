package main

import (
	"context"
	"fmt"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/companion"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
)

func main() {
	runtime := speechkit.NewRuntime(speechkit.Snapshot{}, speechkit.Hooks{})
	defer runtime.Close()

	c, err := companion.NewHandsFree(companion.Options{
		Runtime: runtime,
		WakeRequest: func(_ context.Context, ev wakeword.DetectionEvent) (speechkit.AssistRequest, bool) {
			return speechkit.AssistRequest{Text: ev.Phrase, Locale: "en-US"}, false
		},
	})
	if err != nil {
		panic(err)
	}
	if err := c.Start(context.Background()); err != nil {
		panic(err)
	}
	c.WakeSink().Emit(wakeword.DetectionEvent{Phrase: "Hey Quby", Mode: "assist"})
	fmt.Println((<-c.Events()).Type)
}
