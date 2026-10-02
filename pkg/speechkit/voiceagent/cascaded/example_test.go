package cascaded_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

type noSTT struct{}

func (noSTT) Route(context.Context, []byte, float64, stt.TranscribeOpts) (*stt.Result, error) {
	return &stt.Result{}, nil
}

// quoteAgent stands in for the host's LLM call.
type quoteAgent struct{}

func (quoteAgent) Run(_ context.Context, in cascaded.AgentInput) (cascaded.AgentOutput, error) {
	return cascaded.AgentOutput{Text: "You asked: " + in.Utterance, Action: "display"}, nil
}

// ExampleNewProvider builds a cascaded (STT -> LLM -> TTS) Voice Agent
// provider from host-supplied parts and runs one text turn. Audio input goes
// through SendAudio; TTS is optional and omitted here.
func ExampleNewProvider() {
	p := cascaded.NewProvider(cascaded.Deps{STT: noSTT{}, Agent: quoteAgent{}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Connect(ctx, cascaded.SessionConfig{Locale: "en"}); err != nil {
		log.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	if err := p.SendText("what time is it"); err != nil {
		log.Fatal(err)
	}
	for {
		msg, err := p.Receive(ctx)
		if err != nil {
			log.Fatal(err)
		}
		if msg.OutputTranscriptDone {
			fmt.Println(msg.OutputTranscript)
			break
		}
	}
	// Output: You asked: what time is it
}
