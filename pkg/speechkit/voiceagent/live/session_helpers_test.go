package live

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type sessionTestProvider struct {
	mu        sync.Mutex
	connected bool
	closed    bool
	lastCfg   LiveConfig
	messages  chan *LiveMessage
	sentText  []string
}

type reconnectingSessionTestProvider struct {
	*sessionTestProvider
	reconnects int
}

func newSessionTestProvider() *sessionTestProvider {
	return &sessionTestProvider{
		messages: make(chan *LiveMessage, 8),
	}
}

func (p *sessionTestProvider) Connect(_ context.Context, cfg LiveConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connected = true
	p.lastCfg = cfg
	return nil
}

func (p *sessionTestProvider) SendAudio(_ []byte) error { return nil }

func (p *sessionTestProvider) SendAudioStreamEnd() error { return nil }

func (p *sessionTestProvider) Receive(ctx context.Context) (*LiveMessage, error) {
	select {
	case msg := <-p.messages:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *sessionTestProvider) SendText(text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sentText = append(p.sentText, text)
	return nil
}

func (p *sessionTestProvider) SendToolResponse(_ ToolResponse) error { return nil }

func (p *sessionTestProvider) Name() string { return "session-test" }

func (p *sessionTestProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

type hangingCloseProvider struct {
	*sessionTestProvider
	entered chan struct{}
	release chan struct{}
}

func (p *hangingCloseProvider) Close() error {
	select {
	case <-p.entered:
	default:
		close(p.entered)
	}
	<-p.release
	return p.sessionTestProvider.Close()
}

func (p *reconnectingSessionTestProvider) Reconnect(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reconnects++
	return nil
}

type dialogueRule struct {
	Contains    []string
	NotContains []string
}

type dialogueTurn struct {
	Speaker string
	StepID  string
	Text    string
	Rule    dialogueRule
}

func evaluateDialogueTranscript(turns []dialogueTurn) error {
	for i, turn := range turns {
		text := strings.ToLower(turn.Text)
		for _, want := range turn.Rule.Contains {
			if !strings.Contains(text, strings.ToLower(want)) {
				return fmt.Errorf("turn %d (%s/%s) missing %q in %q", i+1, turn.Speaker, turn.StepID, want, turn.Text)
			}
		}
		for _, forbidden := range turn.Rule.NotContains {
			if strings.Contains(text, strings.ToLower(forbidden)) {
				return fmt.Errorf("turn %d (%s/%s) unexpectedly contains %q in %q", i+1, turn.Speaker, turn.StepID, forbidden, turn.Text)
			}
		}
	}
	return nil
}

type dialogueEvent struct {
	side string
	text string
}

func waitForDialogueEvent(t *testing.T, ch <-chan dialogueEvent, side, text string) {
	t.Helper()
	select {
	case event := <-ch:
		if event.side != side || event.text != text {
			t.Fatalf("dialogue event = %#v, want side=%q text=%q", event, side, text)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s dialogue event %q", side, text)
	}
}

func providerSentText(provider *sessionTestProvider) []string {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]string(nil), provider.sentText...)
}

func providerConfigSnapshot(provider *sessionTestProvider) LiveConfig {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.lastCfg
}

func waitForSentText(t *testing.T, provider *sessionTestProvider) string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		sent := providerSentText(provider)
		if len(sent) > 0 {
			return sent[len(sent)-1]
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for provider text")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func drainStateChanges(ch <-chan State) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func waitForState(t *testing.T, ch <-chan State, want State) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case state := <-ch:
			if state == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", want)
		}
	}
}

func countBufferedState(ch <-chan State, want State) int {
	count := 0
	for {
		select {
		case state := <-ch:
			if state == want {
				count++
			}
		default:
			return count
		}
	}
}

func assertNoStateWithin(t *testing.T, ch <-chan State, forbidden State, timeout time.Duration) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case state := <-ch:
			if state == forbidden {
				t.Fatalf("unexpected %s state change within %s", forbidden, timeout)
			}
		case <-timer.C:
			return
		}
	}
}
