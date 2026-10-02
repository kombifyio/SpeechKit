package voiceagent_test

import (
	"context"
	"fmt"
	"log"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent"
)

// echoProvider is a minimal in-memory voiceagent.Provider. Real hosts use
// voiceagent/local (in-process realtime providers) or their own backend.
type echoProvider struct {
	cb      voiceagent.Callbacks
	session speechkit.VoiceAgentSession
}

func (p *echoProvider) StartVoiceAgent(_ context.Context, cfg voiceagent.Config, cb voiceagent.Callbacks) error {
	p.cb = cb
	p.session.Locale = cfg.Locale
	return nil
}

func (p *echoProvider) SendText(_ context.Context, text string) error {
	p.cb.OnText("you said: " + text)
	return nil
}

func (p *echoProvider) StopVoiceAgent(context.Context) (speechkit.VoiceAgentSession, error) {
	return p.session, nil
}

func (p *echoProvider) CurrentSession(context.Context) (speechkit.VoiceAgentSession, error) {
	return p.session, nil
}

// ExampleNewService wraps a Provider in the embeddable Voice Agent Service:
// Start opens a session, SendText injects a user turn, agent output arrives
// through Callbacks, and Stop returns the session record.
func ExampleNewService() {
	svc, err := voiceagent.NewService(voiceagent.Options{
		Config:    voiceagent.Config{Locale: "en-US", Instruction: "Be brief."},
		Callbacks: voiceagent.Callbacks{OnText: func(s string) { fmt.Println("agent:", s) }},
		Provider:  &echoProvider{},
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		log.Fatal(err)
	}
	if err := svc.SendText(ctx, "hello"); err != nil {
		log.Fatal(err)
	}
	session, err := svc.Stop(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("locale:", session.Locale)
	// Output:
	// agent: you said: hello
	// locale: en-US
}
