package voiceagent

import (
	"context"
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type fakeProvider struct {
	started bool
	text    string
	session speechkit.VoiceAgentSession
}

func (p *fakeProvider) StartVoiceAgent(_ context.Context, cfg Config, callbacks Callbacks) error {
	p.started = true
	p.session.ProviderProfileID = cfg.ProviderProfileID
	p.session.Locale = cfg.Locale
	if callbacks.OnText != nil {
		callbacks.OnText("ready")
	}
	return nil
}

func (p *fakeProvider) StopVoiceAgent(context.Context) (speechkit.VoiceAgentSession, error) {
	p.started = false
	p.session.Summary = speechkit.VoiceAgentSessionSummary{Summary: "done"}
	return p.session, nil
}

func (p *fakeProvider) SendText(_ context.Context, text string) error {
	p.text = text
	return nil
}

func (p *fakeProvider) CurrentSession(context.Context) (speechkit.VoiceAgentSession, error) {
	return p.session, nil
}

func TestServiceWrapsProvider(t *testing.T) {
	provider := &fakeProvider{}
	var callbackText string
	service, err := NewService(Options{
		Config: Config{
			ProviderProfileID: "realtime.google.gemini-native-audio",
			Locale:            "de",
		},
		Callbacks: Callbacks{
			OnText: func(text string) { callbackText = text },
		},
		Provider: provider,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !provider.started {
		t.Fatal("provider was not started")
	}
	if callbackText != "ready" {
		t.Fatalf("callback text = %q, want ready", callbackText)
	}

	if err := service.SendText(context.Background(), "next idea"); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}
	if provider.text != "next idea" {
		t.Fatalf("provider text = %q, want next idea", provider.text)
	}

	session, err := service.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if got, want := session.Summary.Summary, "done"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestServiceRequiresProvider(t *testing.T) {
	_, err := NewService(Options{})
	if !errors.Is(err, ErrMissingProvider) {
		t.Fatalf("NewService() error = %v, want %v", err, ErrMissingProvider)
	}
}
