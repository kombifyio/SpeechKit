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
	mu        sync.Mutex
	connected bool
	closed    bool
	messages  chan *LiveMessage
	sentAudio [][]byte
	sentTexts []string
	connectErr error
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
