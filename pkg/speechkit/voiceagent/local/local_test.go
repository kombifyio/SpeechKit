package local

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// fakeLive is a scriptable live.LiveProvider: the test enqueues
// LiveMessages, the session's receive loop drains them.
type fakeLive struct {
	mu            sync.Mutex
	cfg           live.LiveConfig
	messages      chan *live.LiveMessage
	sentText      []string
	sentAudio     [][]byte
	toolResponses chan live.ToolResponse
	closed        bool
}

func newFakeLive() *fakeLive {
	return &fakeLive{
		messages:      make(chan *live.LiveMessage, 16),
		toolResponses: make(chan live.ToolResponse, 4),
	}
}

func (f *fakeLive) Connect(_ context.Context, cfg live.LiveConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cfg = cfg
	return nil
}

func (f *fakeLive) SendAudio(chunk []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentAudio = append(f.sentAudio, append([]byte(nil), chunk...))
	return nil
}

func (f *fakeLive) SendAudioStreamEnd() error { return nil }

func (f *fakeLive) Receive(ctx context.Context) (*live.LiveMessage, error) {
	select {
	case msg, ok := <-f.messages:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeLive) SendText(text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentText = append(f.sentText, text)
	return nil
}

func (f *fakeLive) SendToolResponse(response live.ToolResponse) error {
	f.toolResponses <- response
	return nil
}

func (f *fakeLive) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.messages)
	}
	return nil
}

func (f *fakeLive) Name() string { return "fake" }

func (f *fakeLive) connectedCfg() live.LiveConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg
}

func newTestProvider(t *testing.T, fake *fakeLive, opts Options) *Provider {
	t.Helper()
	opts.Factory = func(cfg live.LiveConfig) (live.LiveProvider, live.LiveConfig, error) {
		return fake, cfg, nil
	}
	p, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func TestStartMergesConfigAndAnnouncesTools(t *testing.T) {
	fake := newFakeLive()
	registry := agentkit.NewRegistry()
	registry.MustRegister(&agentkit.FuncTool{
		ToolName:        "home_assistant",
		ToolDescription: "Steuert das Smart Home.",
		ToolSchema:      agentkit.Schema{"type": "object"},
		Fn: func(context.Context, map[string]any) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		},
	})

	p := newTestProvider(t, fake, Options{
		Live:  live.LiveConfig{Provider: "deepgram", Model: "base-model", Locale: "en"},
		Tools: registry,
	})

	err := p.StartVoiceAgent(context.Background(), voiceagent.Config{
		Model:       "override-model",
		Locale:      "de-DE",
		Instruction: "Du bist kombify.",
	}, voiceagent.Callbacks{})
	if err != nil {
		t.Fatalf("StartVoiceAgent: %v", err)
	}
	defer func() { _, _ = p.StopVoiceAgent(context.Background()) }()

	cfg := fake.connectedCfg()
	if cfg.Model != "override-model" || cfg.Locale != "de-DE" || cfg.FrameworkPrompt != "Du bist kombify." {
		t.Fatalf("merged cfg = %+v", cfg)
	}
	if len(cfg.Tools) != 1 || cfg.Tools[0].Name != "home_assistant" {
		t.Fatalf("tools not announced: %+v", cfg.Tools)
	}

	if err := p.StartVoiceAgent(context.Background(), voiceagent.Config{}, voiceagent.Callbacks{}); !errors.Is(err, ErrSessionActive) {
		t.Fatalf("second start = %v, want ErrSessionActive", err)
	}
}

func TestToolCallRoundTripAndTurnRecording(t *testing.T) {
	fake := newFakeLive()
	registry := agentkit.NewRegistry()
	var gotArgs map[string]any
	registry.MustRegister(&agentkit.FuncTool{
		ToolName:        "home_assistant",
		ToolDescription: "Steuert das Smart Home.",
		ToolSchema:      agentkit.Schema{"type": "object"},
		Fn: func(_ context.Context, args map[string]any) (map[string]any, error) {
			gotArgs = args
			return map[string]any{"status": "ok", "speak_hint": "Licht ist an."}, nil
		},
	})

	// Callbacks laufen auf der Receive-Loop-Goroutine — synchronisiert lesen.
	var cbMu sync.Mutex
	var audioOut [][]byte
	var interrupted bool
	p := newTestProvider(t, fake, Options{
		Live:  live.LiveConfig{Provider: "deepgram"},
		Tools: registry,
		Callbacks: live.Callbacks{
			OnAudio: func(a []byte) {
				cbMu.Lock()
				audioOut = append(audioOut, a)
				cbMu.Unlock()
			},
			OnInterrupted: func() {
				cbMu.Lock()
				interrupted = true
				cbMu.Unlock()
			},
		},
	})

	if err := p.StartVoiceAgent(context.Background(), voiceagent.Config{}, voiceagent.Callbacks{}); err != nil {
		t.Fatalf("StartVoiceAgent: %v", err)
	}

	// Scripted conversation: user asks, agent calls the tool, agent answers
	// with audio + transcript, user barge-in.
	fake.messages <- &live.LiveMessage{InputTranscript: "schalte das licht an", InputTranscriptDone: true}
	fake.messages <- &live.LiveMessage{ToolCalls: []live.ToolCall{{
		ID: "t1", Name: "home_assistant", Args: map[string]any{"query": "licht an"},
	}}}
	fake.messages <- &live.LiveMessage{Audio: []byte{1, 2, 3}, OutputTranscript: "Erledigt, Licht ist an.", OutputTranscriptDone: true}
	fake.messages <- &live.LiveMessage{Interrupted: true}

	select {
	case resp := <-fake.toolResponses:
		if resp.ID != "t1" || resp.Name != "home_assistant" {
			t.Fatalf("tool response = %+v", resp)
		}
		if resp.Response["status"] != "ok" {
			t.Fatalf("tool response payload = %+v", resp.Response)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for tool response")
	}
	if gotArgs["query"] != "licht an" {
		t.Fatalf("tool args = %+v", gotArgs)
	}

	waitFor(t, "audio + turns + interrupt", func() bool {
		snap, err := p.CurrentSession(context.Background())
		cbMu.Lock()
		gotAudio, gotInterrupt := len(audioOut) == 1, interrupted
		cbMu.Unlock()
		return err == nil && len(snap.Turns) == 2 && gotAudio && gotInterrupt
	})

	record, err := p.StopVoiceAgent(context.Background())
	if err != nil {
		t.Fatalf("StopVoiceAgent: %v", err)
	}
	if record.RuntimeKind != RuntimeKindLocal {
		t.Fatalf("RuntimeKind = %q", record.RuntimeKind)
	}
	if record.EndedAt.IsZero() || record.StartedAt.IsZero() {
		t.Fatalf("timestamps missing: %+v", record)
	}
	if len(record.Turns) != 2 || record.Turns[0].Role != "user" || record.Turns[1].Role != "assistant" {
		t.Fatalf("turns = %+v", record.Turns)
	}
}

func TestSendWithoutSessionErrors(t *testing.T) {
	p := newTestProvider(t, newFakeLive(), Options{Live: live.LiveConfig{Provider: "deepgram"}})

	if err := p.SendText(context.Background(), "hallo"); !errors.Is(err, ErrNoActiveSession) {
		t.Fatalf("SendText = %v, want ErrNoActiveSession", err)
	}
	if err := p.SendAudio([]byte{0, 1}); !errors.Is(err, ErrNoActiveSession) {
		t.Fatalf("SendAudio = %v, want ErrNoActiveSession", err)
	}
	if _, err := p.CurrentSession(context.Background()); !errors.Is(err, ErrNoActiveSession) {
		t.Fatalf("CurrentSession = %v, want ErrNoActiveSession", err)
	}
	if _, err := p.StopVoiceAgent(context.Background()); !errors.Is(err, ErrNoActiveSession) {
		t.Fatalf("StopVoiceAgent = %v, want ErrNoActiveSession", err)
	}
}

func TestAudioPassthroughAndRestart(t *testing.T) {
	fake := newFakeLive()
	p := newTestProvider(t, fake, Options{Live: live.LiveConfig{Provider: "deepgram"}})

	if err := p.StartVoiceAgent(context.Background(), voiceagent.Config{}, voiceagent.Callbacks{}); err != nil {
		t.Fatalf("StartVoiceAgent: %v", err)
	}
	if err := p.SendAudio([]byte{9, 9}); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}
	waitFor(t, "audio forwarded", func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return len(fake.sentAudio) == 1
	})
	if _, err := p.StopVoiceAgent(context.Background()); err != nil {
		t.Fatalf("StopVoiceAgent: %v", err)
	}

	// After stop, a fresh session must start cleanly.
	fake2 := newFakeLive()
	p.opts.Factory = func(cfg live.LiveConfig) (live.LiveProvider, live.LiveConfig, error) {
		return fake2, cfg, nil
	}
	if err := p.StartVoiceAgent(context.Background(), voiceagent.Config{}, voiceagent.Callbacks{}); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if _, err := p.StopVoiceAgent(context.Background()); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}
