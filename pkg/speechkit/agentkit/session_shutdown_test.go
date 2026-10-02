package agentkit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"go.uber.org/goleak"
)

func TestAgentSessionShutdownWaitsForToolsAndHonoursContext(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	provider := newTestLiveProvider()
	registry := NewRegistry()
	entered := make(chan struct{})
	release := make(chan struct{})
	if err := registry.Register(&FuncTool{
		ToolName:   "block",
		ToolSchema: Schema{"type": "object"},
		Fn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
			close(entered)
			<-release // ignores ctx: models an uncooperative tool
			return map[string]any{}, nil
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	session := NewAgentSession(provider, Callbacks{}, registry, LifecycleHooks{}, nil)
	if err := session.Start(context.Background(), LiveConfig{}, live.DefaultIdleConfig()); err != nil {
		t.Fatalf("start: %v", err)
	}
	provider.messages <- &live.LiveMessage{ToolCalls: []ToolCall{{ID: "c1", Name: "block"}}}
	<-entered

	short, cancelShort := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelShort()
	if err := session.Shutdown(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown with blocked tool = %v, want deadline exceeded", err)
	}

	close(release)
	long, cancelLong := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelLong()
	if err := session.Shutdown(long); err != nil {
		t.Fatalf("shutdown after release: %v", err)
	}
	select {
	case r := <-provider.responses:
		t.Fatalf("tool result sent after shutdown: %#v", r)
	default:
	}
}
