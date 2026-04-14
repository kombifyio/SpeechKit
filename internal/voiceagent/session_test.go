package voiceagent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// mockLiveProvider implements LiveProvider for testing.
type mockLiveProvider struct {
	mu            sync.Mutex
	connected     bool
	closed        bool
	messages      chan *LiveMessage
	sentAudio     [][]byte
	sentTexts     []string
	toolResponses []ToolResponse
	connectErr    error
}

func newMockLiveProvider() *mockLiveProvider {
	return &mockLiveProvider{
		messages: make(chan *LiveMessage, 10),
	}
}

func (m *mockLiveProvider) Connect(_ context.Context, _ LiveConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}

func (m *mockLiveProvider) SendAudio(chunk []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return fmt.Errorf("not connected")
	}
	m.sentAudio = append(m.sentAudio, chunk)
	return nil
}

func (m *mockLiveProvider) Receive(ctx context.Context) (*LiveMessage, error) {
	select {
	case msg := <-m.messages:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *mockLiveProvider) SendText(text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentTexts = append(m.sentTexts, text)
	return nil
}

func (m *mockLiveProvider) SendToolResponse(response ToolResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolResponses = append(m.toolResponses, response)
	return nil
}

func (m *mockLiveProvider) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.connected = false
	return nil
}

func (m *mockLiveProvider) Name() string { return "mock-live" }

func TestSessionStartStop(t *testing.T) {
	mock := newMockLiveProvider()
	var stateChanges []State
	session := NewSession(mock, Callbacks{
		OnStateChange: func(s State) {
			stateChanges = append(stateChanges, s)
		},
	})

	if session.CurrentState() != StateInactive {
		t.Fatalf("expected inactive, got %s", session.CurrentState())
	}

	ctx := context.Background()
	err := session.Start(ctx, LiveConfig{
		Model:  "test-model",
		APIKey: "test-key",
		Locale: "en",
	}, IdleConfig{
		ReminderAfter:   1 * time.Hour, // Long timeout to avoid interference.
		DeactivateAfter: 2 * time.Hour,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if session.CurrentState() != StateListening {
		t.Errorf("expected listening, got %s", session.CurrentState())
	}

	session.Stop()

	if session.CurrentState() != StateInactive {
		t.Errorf("expected inactive after stop, got %s", session.CurrentState())
	}

	if !mock.closed {
		t.Error("expected provider to be closed")
	}
}

func TestSessionConnectFailure(t *testing.T) {
	mock := newMockLiveProvider()
	mock.connectErr = fmt.Errorf("connection refused")

	session := NewSession(mock, Callbacks{})

	err := session.Start(context.Background(), LiveConfig{}, DefaultIdleConfig())
	if err == nil {
		t.Fatal("expected error on connect failure")
	}

	if session.CurrentState() != StateInactive {
		t.Errorf("expected inactive after failure, got %s", session.CurrentState())
	}
}

func TestSessionSendAudio(t *testing.T) {
	mock := newMockLiveProvider()
	session := NewSession(mock, Callbacks{})

	ctx := context.Background()
	if err := session.Start(ctx, LiveConfig{Locale: "de"}, IdleConfig{
		ReminderAfter:   1 * time.Hour,
		DeactivateAfter: 2 * time.Hour,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	chunk := []byte{0x01, 0x02, 0x03}
	if err := session.SendAudio(chunk); err != nil {
		t.Fatalf("send audio: %v", err)
	}

	mock.mu.Lock()
	if len(mock.sentAudio) != 1 {
		t.Errorf("expected 1 audio chunk sent, got %d", len(mock.sentAudio))
	}
	mock.mu.Unlock()

	session.Stop()
}

func TestSessionReceiveAudioAndText(t *testing.T) {
	mock := newMockLiveProvider()
	var receivedAudio []byte
	var receivedText string
	var mu sync.Mutex

	session := NewSession(mock, Callbacks{
		OnAudio: func(audio []byte) {
			mu.Lock()
			receivedAudio = append(receivedAudio, audio...)
			mu.Unlock()
		},
		OnText: func(text string) {
			mu.Lock()
			receivedText += text
			mu.Unlock()
		},
	})

	ctx := context.Background()
	if err := session.Start(ctx, LiveConfig{Locale: "en"}, IdleConfig{
		ReminderAfter:   1 * time.Hour,
		DeactivateAfter: 2 * time.Hour,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Simulate model response.
	mock.messages <- &LiveMessage{
		Audio: []byte{0x10, 0x20},
		Text:  "Hello there",
		Done:  true,
	}

	// Wait for receive loop to process.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(receivedAudio) != 2 {
		t.Errorf("expected 2 audio bytes, got %d", len(receivedAudio))
	}
	if receivedText != "Hello there" {
		t.Errorf("expected 'Hello there', got '%s'", receivedText)
	}
	mu.Unlock()

	session.Stop()
}

func TestSessionDoubleStart(t *testing.T) {
	mock := newMockLiveProvider()
	session := NewSession(mock, Callbacks{})

	ctx := context.Background()
	if err := session.Start(ctx, LiveConfig{}, IdleConfig{
		ReminderAfter:   1 * time.Hour,
		DeactivateAfter: 2 * time.Hour,
	}); err != nil {
		t.Fatalf("first start: %v", err)
	}

	err := session.Start(ctx, LiveConfig{}, DefaultIdleConfig())
	if err == nil {
		t.Fatal("expected error on double start")
	}

	session.Stop()
}

func TestSessionTranscriptionCallbacks(t *testing.T) {
	mock := newMockLiveProvider()
	var inputTexts []string
	var outputTexts []string
	var inputDones []bool
	var outputDones []bool
	var mu sync.Mutex

	session := NewSession(mock, Callbacks{
		OnInputTranscript: func(text string, done bool) {
			mu.Lock()
			inputTexts = append(inputTexts, text)
			inputDones = append(inputDones, done)
			mu.Unlock()
		},
		OnOutputTranscript: func(text string, done bool) {
			mu.Lock()
			outputTexts = append(outputTexts, text)
			outputDones = append(outputDones, done)
			mu.Unlock()
		},
	})

	ctx := context.Background()
	if err := session.Start(ctx, LiveConfig{Locale: "en"}, IdleConfig{
		ReminderAfter:   1 * time.Hour,
		DeactivateAfter: 2 * time.Hour,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Simulate input transcription (user speech).
	mock.messages <- &LiveMessage{
		InputTranscript:     "Hello",
		InputTranscriptDone: false,
	}
	// Simulate finalized input transcription.
	mock.messages <- &LiveMessage{
		InputTranscript:     "Hello world",
		InputTranscriptDone: true,
	}
	// Simulate output transcription (model speech).
	mock.messages <- &LiveMessage{
		OutputTranscript:     "Hi there",
		OutputTranscriptDone: true,
		Done:                 true,
	}

	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	if len(inputTexts) != 2 {
		t.Errorf("expected 2 input transcripts, got %d", len(inputTexts))
	}
	if len(inputTexts) >= 2 && inputTexts[1] != "Hello world" {
		t.Errorf("expected 'Hello world', got '%s'", inputTexts[1])
	}
	if len(inputDones) >= 2 && !inputDones[1] {
		t.Error("expected second input transcript to be done")
	}
	if len(outputTexts) != 1 {
		t.Errorf("expected 1 output transcript, got %d", len(outputTexts))
	}
	if len(outputTexts) >= 1 && outputTexts[0] != "Hi there" {
		t.Errorf("expected 'Hi there', got '%s'", outputTexts[0])
	}
	mu.Unlock()

	session.Stop()
}

func TestSessionInterruptedCallback(t *testing.T) {
	mock := newMockLiveProvider()
	var interrupted bool
	var stateChanges []State
	var mu sync.Mutex

	session := NewSession(mock, Callbacks{
		OnStateChange: func(s State) {
			mu.Lock()
			stateChanges = append(stateChanges, s)
			mu.Unlock()
		},
		OnInterrupted: func() {
			mu.Lock()
			interrupted = true
			mu.Unlock()
		},
	})

	ctx := context.Background()
	if err := session.Start(ctx, LiveConfig{Locale: "en"}, IdleConfig{
		ReminderAfter:   1 * time.Hour,
		DeactivateAfter: 2 * time.Hour,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Simulate barge-in.
	mock.messages <- &LiveMessage{Interrupted: true}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if !interrupted {
		t.Error("expected OnInterrupted callback to fire")
	}
	// Should have transitioned to listening after interrupt.
	found := false
	for _, s := range stateChanges {
		if s == StateListening {
			found = true
		}
	}
	if !found {
		t.Error("expected StateListening after interrupt")
	}
	mu.Unlock()

	session.Stop()
}

func TestSessionReceiveErrorCleansUp(t *testing.T) {
	mock := newMockLiveProvider()
	var receivedError error
	var sessionEnded bool
	var finalState State
	var mu sync.Mutex

	session := NewSession(mock, Callbacks{
		OnError: func(err error) {
			mu.Lock()
			receivedError = err
			mu.Unlock()
		},
		OnSessionEnd: func() {
			mu.Lock()
			sessionEnded = true
			mu.Unlock()
		},
		OnStateChange: func(s State) {
			mu.Lock()
			finalState = s
			mu.Unlock()
		},
	})

	ctx := context.Background()
	if err := session.Start(ctx, LiveConfig{Locale: "en"}, IdleConfig{
		ReminderAfter:   1 * time.Hour,
		DeactivateAfter: 2 * time.Hour,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Close the messages channel to simulate a receive error.
	close(mock.messages)

	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	if receivedError == nil {
		t.Error("expected OnError callback to fire on receive error")
	}
	if !sessionEnded {
		t.Error("expected OnSessionEnd callback to fire on receive error")
	}
	if finalState != StateInactive {
		t.Errorf("expected final state Inactive, got %s", finalState)
	}
	mu.Unlock()

	if !mock.closed {
		t.Error("expected provider to be closed after receive error")
	}
}

func TestSessionGoAwayWithoutReconnectCleansUp(t *testing.T) {
	mock := newMockLiveProvider()
	var sessionEnded bool
	var mu sync.Mutex

	session := NewSession(mock, Callbacks{
		OnSessionEnd: func() {
			mu.Lock()
			sessionEnded = true
			mu.Unlock()
		},
	})

	ctx := context.Background()
	if err := session.Start(ctx, LiveConfig{Locale: "en"}, IdleConfig{
		ReminderAfter:   1 * time.Hour,
		DeactivateAfter: 2 * time.Hour,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Send GoAway — mock doesn't implement LiveReconnector, so it should clean up.
	mock.messages <- &LiveMessage{GoAway: true}

	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	if !sessionEnded {
		t.Error("expected OnSessionEnd for GoAway without reconnect support")
	}
	mu.Unlock()

	if session.CurrentState() != StateInactive {
		t.Errorf("expected Inactive after GoAway, got %s", session.CurrentState())
	}
}

func TestSessionOnSessionEndNotCalledOnManualStop(t *testing.T) {
	mock := newMockLiveProvider()
	var sessionEnded bool
	var mu sync.Mutex

	session := NewSession(mock, Callbacks{
		OnSessionEnd: func() {
			mu.Lock()
			sessionEnded = true
			mu.Unlock()
		},
	})

	ctx := context.Background()
	if err := session.Start(ctx, LiveConfig{Locale: "en"}, IdleConfig{
		ReminderAfter:   1 * time.Hour,
		DeactivateAfter: 2 * time.Hour,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	session.Stop()

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if sessionEnded {
		t.Error("OnSessionEnd should NOT fire on manual Stop (only on errors/GoAway)")
	}
	mu.Unlock()
}

func TestSessionDeliversToolCallsToHost(t *testing.T) {
	mock := newMockLiveProvider()
	var calls []ToolCall
	var mu sync.Mutex

	session := NewSession(mock, Callbacks{
		OnToolCall: func(call ToolCall) {
			mu.Lock()
			calls = append(calls, call)
			mu.Unlock()
		},
	})

	ctx := context.Background()
	if err := session.Start(ctx, LiveConfig{Locale: "en"}, IdleConfig{
		ReminderAfter:   1 * time.Hour,
		DeactivateAfter: 2 * time.Hour,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	mock.messages <- &LiveMessage{
		ToolCalls: []ToolCall{
			{
				ID:   "call-1",
				Name: "extract_result",
				Args: map[string]any{"format": "summary"},
			},
		},
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "extract_result" {
		t.Fatalf("tool call name = %q", calls[0].Name)
	}
	mu.Unlock()

	session.Stop()
}
