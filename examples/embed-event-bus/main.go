package main

import (
	"fmt"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func main() {
	runtime := speechkit.NewRuntime(speechkit.Snapshot{}, speechkit.Hooks{})
	defer runtime.Close()

	runtime.Publish(speechkit.Event{
		Type:    speechkit.EventWakeFired,
		Mode:    "voice_agent",
		Message: "wake phrase fired",
	})
	event := <-runtime.Events()
	fmt.Println(event.Type, event.Mode)
}
