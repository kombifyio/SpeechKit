package live

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type orderedIdleProvider struct {
	mu    sync.Mutex
	order []string
	err   error
}

func (*orderedIdleProvider) Connect(context.Context, LiveConfig) error { return nil }
func (*orderedIdleProvider) SendAudio([]byte) error                    { return nil }
func (*orderedIdleProvider) SendAudioStreamEnd() error                 { return nil }
func (*orderedIdleProvider) Receive(context.Context) (*LiveMessage, error) {
	return nil, context.Canceled
}
func (*orderedIdleProvider) SendToolResponse(ToolResponse) error { return nil }
func (*orderedIdleProvider) Close() error                        { return nil }
func (*orderedIdleProvider) Name() string                        { return "ordered-idle" }

func (p *orderedIdleProvider) SendText(string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.order = append(p.order, "send")
	return p.err
}

func TestReminderPromptUsesConfiguredDuration(t *testing.T) {
	got := reminderPrompt("en", 2*time.Minute)
	if !strings.Contains(got, "2 minutes") {
		t.Fatalf("reminder prompt = %q, want configured duration", got)
	}
}

func TestReminderPromptFallsBackToDefaultDuration(t *testing.T) {
	got := reminderPrompt("en", 0)
	if !strings.Contains(got, "5 minutes") {
		t.Fatalf("reminder prompt = %q, want default duration", got)
	}
}

func TestReminderPromptUsesGermanLocale(t *testing.T) {
	got := reminderPrompt("de-DE", 3*time.Minute)
	if !strings.Contains(got, "3 Minuten") {
		t.Fatalf("german reminder prompt = %q, want configured german duration", got)
	}
}

func TestDeactivatePromptUsesLocale(t *testing.T) {
	if got := deactivatePrompt("de"); !strings.Contains(got, "Inaktivitaet") {
		t.Fatalf("german deactivate prompt = %q", got)
	}
	if got := deactivatePrompt("en"); !strings.Contains(got, "inactivity") {
		t.Fatalf("english deactivate prompt = %q", got)
	}
}

func TestHostPromptCallbackPrecedesIdleProviderSend(t *testing.T) {
	provider := &orderedIdleProvider{}
	session := &Session{provider: provider}
	session.callbacks.OnHostPrompt = func(event HostPromptEvent) bool {
		if event.Kind != HostPromptIdleReminder || event.Type != HostPromptStarted || event.ID == 0 {
			if event.Type != HostPromptSent {
				t.Fatalf("host prompt event = %#v", event)
			}
			return true
		}
		provider.mu.Lock()
		provider.order = append(provider.order, "callback")
		provider.mu.Unlock()
		return true
	}
	if err := session.sendHostPrompt(HostPromptIdleReminder, "still there?"); err != nil {
		t.Fatalf("sendHostPrompt: %v", err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if got := strings.Join(provider.order, ","); got != "callback,send" {
		t.Fatalf("order = %q", got)
	}
}

func TestHostPromptSendFailureIsCorrelated(t *testing.T) {
	provider := &orderedIdleProvider{err: context.Canceled}
	session := &Session{provider: provider}
	var events []HostPromptEvent
	session.callbacks.OnHostPrompt = func(event HostPromptEvent) bool {
		events = append(events, event)
		return true
	}
	if err := session.sendHostPrompt(HostPromptIdleDeactivate, "goodbye"); err == nil {
		t.Fatal("sendHostPrompt unexpectedly succeeded")
	}
	if len(events) != 2 || events[0].Type != HostPromptStarted || events[1].Type != HostPromptSendFailed ||
		events[0].ID == 0 || events[0].ID != events[1].ID || events[0].Kind != events[1].Kind {
		t.Fatalf("host prompt events = %#v", events)
	}
}

func TestHostPromptRejectionSkipsProviderSend(t *testing.T) {
	provider := &orderedIdleProvider{}
	session := &Session{provider: provider}
	session.callbacks.OnHostPrompt = func(event HostPromptEvent) bool {
		return event.Type != HostPromptStarted
	}
	if err := session.sendHostPrompt(HostPromptIdleReminder, "still there?"); !errors.Is(err, errHostPromptRejected) {
		t.Fatalf("sendHostPrompt error = %v", err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.order) != 0 {
		t.Fatalf("rejected prompt reached provider: %v", provider.order)
	}
}
