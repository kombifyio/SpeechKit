package live_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// echoLive is an in-memory live.LiveProvider. Real hosts use a provider from
// voiceagent/live/openai, deepgram, assemblyai, or allproviders.
type echoLive struct{ out chan *live.LiveMessage }

func (p *echoLive) Connect(context.Context, live.LiveConfig) error { return nil }
func (p *echoLive) SendAudio([]byte) error                         { return nil }
func (p *echoLive) SendAudioStreamEnd() error                      { return nil }
func (p *echoLive) SendToolResponse(live.ToolResponse) error       { return nil }
func (p *echoLive) Close() error                                   { return nil }
func (p *echoLive) Name() string                                   { return "echo" }

func (p *echoLive) SendText(text string) error {
	p.out <- &live.LiveMessage{OutputTranscript: "echo: " + text, OutputTranscriptDone: true, Done: true}
	return nil
}

func (p *echoLive) Receive(ctx context.Context) (*live.LiveMessage, error) {
	select {
	case m := <-p.out:
		return m, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ExampleNewSession drives a realtime dialogue through a Session: Start
// connects the provider, SendText (or SendAudio) feeds the model, and
// transcripts, audio and state changes arrive through Callbacks.
func ExampleNewSession() {
	provider := &echoLive{out: make(chan *live.LiveMessage, 4)}
	heard := make(chan string, 1)
	session := live.NewSession(provider, live.Callbacks{
		OnOutputTranscript: func(text string, done bool) {
			if done {
				heard <- text
			}
		},
	})
	if err := session.Start(context.Background(), live.LiveConfig{Locale: "en"}, live.DefaultIdleConfig()); err != nil {
		log.Fatal(err)
	}
	defer session.Stop()

	if err := session.SendText("hello"); err != nil {
		log.Fatal(err)
	}
	select {
	case text := <-heard:
		fmt.Println(text)
	case <-time.After(5 * time.Second):
		log.Fatal("no answer")
	}
	// Output: echo: hello
}
