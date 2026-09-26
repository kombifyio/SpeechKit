// Package livecontract provides reusable conformance checks for LiveProvider
// implementations.
package livecontract

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// Case configures one conformance run. NewProvider is required and must
// return a fresh, unconnected provider. Config is passed to Connect; Audio,
// Text and ToolResponse are the payloads sent (defaults: four bytes, "ping"
// and a "noop" result); WantName is the expected Name and WantEventType an
// event type the first received message must carry, both optional;
// ReceiveTimeout bounds Connect and Receive (default 2 s).
type Case struct {
	NewProvider    func() live.LiveProvider
	Config         live.LiveConfig
	Audio          []byte
	Text           string
	ToolResponse   live.ToolResponse
	WantName       string
	WantEventType  live.LiveEventType
	ReceiveTimeout time.Duration
}

// Run exercises the whole [live.LiveProvider] surface on a fresh provider: a
// non-empty Name, Connect, SendAudio, SendAudioStreamEnd, SendText,
// SendToolResponse, one Receive that must yield a non-empty message (carrying
// WantEventType when set), and Close twice, the second call proving
// idempotence. It calls t.Fatal on the first violation.
func Run(t testing.TB, c Case) {
	t.Helper()
	if c.NewProvider == nil {
		t.Fatal("livecontract: NewProvider is required")
	}
	provider := c.NewProvider()
	if provider == nil {
		t.Fatal("livecontract: provider is nil")
	}
	if got := strings.TrimSpace(provider.Name()); got == "" {
		t.Fatal("livecontract: provider Name must be non-empty")
	} else if c.WantName != "" && got != c.WantName {
		t.Fatalf("livecontract: provider Name = %q, want %q", got, c.WantName)
	}

	timeout := c.ReceiveTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := provider.Connect(ctx, c.Config); err != nil {
		t.Fatalf("livecontract: Connect: %v", err)
	}
	audio := c.Audio
	if len(audio) == 0 {
		audio = []byte{1, 2, 3, 4}
	}
	if err := provider.SendAudio(audio); err != nil {
		t.Fatalf("livecontract: SendAudio: %v", err)
	}
	if err := provider.SendAudioStreamEnd(); err != nil {
		t.Fatalf("livecontract: SendAudioStreamEnd: %v", err)
	}
	text := strings.TrimSpace(c.Text)
	if text == "" {
		text = "ping"
	}
	if err := provider.SendText(text); err != nil {
		t.Fatalf("livecontract: SendText: %v", err)
	}
	response := c.ToolResponse
	if strings.TrimSpace(response.ID) == "" {
		response = live.ToolResponse{
			ID:       "tool-1",
			Name:     "noop",
			Response: map[string]any{"ok": true},
		}
	}
	if err := provider.SendToolResponse(response); err != nil {
		t.Fatalf("livecontract: SendToolResponse: %v", err)
	}
	msg, err := provider.Receive(ctx)
	if err != nil {
		t.Fatalf("livecontract: Receive: %v", err)
	}
	if msg == nil {
		t.Fatal("livecontract: Receive returned nil message")
		return
	}
	if len(msg.Audio) == 0 && strings.TrimSpace(msg.Text) == "" && len(msg.ToolCalls) == 0 &&
		msg.EventType == "" && len(msg.EventTypes) == 0 && !msg.Done && !msg.GoAway {
		t.Fatalf("livecontract: Receive returned empty message: %+v", msg)
	}
	if c.WantEventType != "" {
		types := live.InferLiveEventTypes(msg)
		if !containsLiveEventType(types, c.WantEventType) {
			t.Fatalf("livecontract: Receive event types = %v, want %s", types, c.WantEventType)
		}
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("livecontract: Close: %v", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("livecontract: second Close must be idempotent: %v", err)
	}
}

func containsLiveEventType(values []live.LiveEventType, want live.LiveEventType) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// RunReceiveCancellation checks that Receive returns an error satisfying
// errors.Is(err, context.Canceled) once its context is cancelled after
// Connect, and that Close still succeeds afterwards.
func RunReceiveCancellation(t testing.TB, c Case) {
	t.Helper()
	if c.NewProvider == nil {
		t.Fatal("livecontract: NewProvider is required")
	}
	provider := c.NewProvider()
	if provider == nil {
		t.Fatal("livecontract: provider is nil")
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := provider.Connect(ctx, c.Config); err != nil {
		t.Fatalf("livecontract: Connect: %v", err)
	}
	cancel()
	if _, err := provider.Receive(ctx); !errors.Is(err, context.Canceled) {
		_ = provider.Close()
		t.Fatalf("livecontract: Receive with canceled context error = %v, want context.Canceled", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("livecontract: Close after canceled receive: %v", err)
	}
}
