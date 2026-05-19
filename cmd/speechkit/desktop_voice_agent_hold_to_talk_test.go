package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/hotkey"
	"github.com/kombifyio/SpeechKit/internal/voiceagent"
)

// slowConnectMockLiveProvider is a voiceagent.LiveProvider whose Connect call
// blocks until the test signals it via releaseConnect(). It lets the
// hold-to-talk regression tests exercise the race between a mic frame
// captured during the WebSocket handshake and the activation goroutine
// transitioning the session out of StateInactive — which is exactly the
// window where the old activate-then-bind code path used to drop audio.
type slowConnectMockLiveProvider struct {
	mu             sync.Mutex
	name           string
	connectGate    chan struct{}
	connected      bool
	closed         bool
	sentAudio      int
	sentAudioBytes []byte
	audioEndCount  int
	messages       chan *voiceagent.LiveMessage
}

func newSlowConnectMockLiveProvider() *slowConnectMockLiveProvider {
	return &slowConnectMockLiveProvider{
		name:        "slow-connect-mock",
		connectGate: make(chan struct{}),
		messages:    make(chan *voiceagent.LiveMessage, 16),
	}
}

func (m *slowConnectMockLiveProvider) releaseConnect() {
	m.mu.Lock()
	gate := m.connectGate
	m.connectGate = nil
	m.mu.Unlock()
	if gate != nil {
		close(gate)
	}
}

func (m *slowConnectMockLiveProvider) Connect(ctx context.Context, _ voiceagent.LiveConfig) error {
	m.mu.Lock()
	gate := m.connectGate
	m.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.mu.Lock()
	m.connected = true
	m.mu.Unlock()
	return nil
}

func (m *slowConnectMockLiveProvider) SendAudio(chunk []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentAudio++
	m.sentAudioBytes = append(m.sentAudioBytes, chunk...)
	return nil
}

func (m *slowConnectMockLiveProvider) SendAudioStreamEnd() error {
	m.mu.Lock()
	m.audioEndCount++
	m.mu.Unlock()
	m.messages <- &voiceagent.LiveMessage{Done: true}
	return nil
}

func (m *slowConnectMockLiveProvider) Receive(ctx context.Context) (*voiceagent.LiveMessage, error) {
	select {
	case msg := <-m.messages:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *slowConnectMockLiveProvider) SendText(string) error { return nil }

func (m *slowConnectMockLiveProvider) SendToolResponse(voiceagent.ToolResponse) error { return nil }

func (m *slowConnectMockLiveProvider) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	if m.connectGate != nil {
		close(m.connectGate)
		m.connectGate = nil
	}
	return nil
}

func (m *slowConnectMockLiveProvider) Name() string { return m.name }

func (m *slowConnectMockLiveProvider) sendAudioCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sentAudio
}

func (m *slowConnectMockLiveProvider) capturedAudioBytes() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]byte, len(m.sentAudioBytes))
	copy(out, m.sentAudioBytes)
	return out
}

func (m *slowConnectMockLiveProvider) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// TestVoiceAgentHoldToTalkBuffersFramesDuringConnect proves the regression
// guard for the activation-lag bug: mic frames captured between the
// hold-to-talk KeyDown and the moment Gemini Live finishes its WebSocket
// handshake must reach the provider once the session is ready. Before the
// pre-session capture fix those frames went into a nil handler (the
// bindVoiceAgentAudio sequence ran after session.Start returned) so the AI
// only heard whatever the user said AFTER the connect window — which felt
// like the agent activated on key release.
func TestVoiceAgentHoldToTalkBuffersFramesDuringConnect(t *testing.T) {
	mockAudio := &mockAudioFrameStreamer{}
	mockProvider := newSlowConnectMockLiveProvider()
	session := voiceagent.NewSession(mockProvider, voiceagent.Callbacks{})

	controller := desktopInputController{
		commands:          &testDesktopCommandBus{},
		recording:         &mutableRecordingState{},
		voiceAgentSession: session,
		voiceAgentConfig:  &config.VoiceAgentConfig{HoldReleaseGraceSec: 1},
		cfg: &config.Config{
			General: config.GeneralConfig{
				VoiceAgentHotkeyBehavior: config.HotkeyBehaviorHoldToTalk,
			},
			Providers: config.ProvidersConfig{
				Google: config.GoogleProviderConfig{APIKeyEnv: "FAKE_KEY_FOR_HOLD_TO_TALK_BUFFER_TEST"},
			},
		},
		audioCapturer: mockAudio,
	}
	t.Setenv("FAKE_KEY_FOR_HOLD_TO_TALK_BUFFER_TEST", "test-api-key")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	controller.handleHotkey(ctx, hotkey.Event{Binding: "voice_agent", Type: hotkey.EventKeyDown})

	// Wait for the activation path to install the microphone handler. This is
	// the synchronous arm step; if it ever became async again this poll would
	// time out and the test would surface the regression.
	waitForCondition(t, 2*time.Second, func() bool { return mockAudio.getHandler() != nil })

	handler := mockAudio.getHandler()
	if handler == nil {
		t.Fatal("expected microphone handler to be installed synchronously on KeyDown")
	}

	// Verify the session is still in StateInactive (or StateConnecting) — the
	// activation goroutine is blocked inside provider.Connect because we have
	// not released the gate yet.
	if state := session.CurrentState(); state != voiceagent.StateInactive && state != voiceagent.StateConnecting {
		t.Fatalf("expected session to be Inactive/Connecting during gated connect, got %s", state)
	}

	// Capture two frames while the WebSocket handshake is in flight. These
	// would have been dropped by the old code path because
	// voiceAgentMicFrameAllowed treats StateInactive as muted.
	handler([]byte{0xAA, 0x00})
	handler([]byte{0xBB, 0x00})

	// Now let Connect finish. The sender goroutine should drain both frames
	// in order before any subsequent capture lands.
	mockProvider.releaseConnect()

	waitForVoiceAgentActive(t, session)
	waitForCondition(t, 2*time.Second, func() bool { return mockProvider.sendAudioCount() >= 2 })

	if got := mockProvider.sendAudioCount(); got < 2 {
		t.Fatalf("expected pre-session frames to reach provider, got sendAudioCount=%d", got)
	}
	if want := []byte{0xAA, 0x00, 0xBB, 0x00}; string(mockProvider.capturedAudioBytes()) != string(want) {
		t.Fatalf("pre-session frames out of order or lost: got %v, want %v", mockProvider.capturedAudioBytes(), want)
	}

	session.Stop()
}

// TestVoiceAgentHoldToTalkReleaseDuringConnectAbortsActivation proves the
// path where the user releases the hold-to-talk shortcut before Gemini Live
// has finished its WebSocket handshake. The session should not stall on the
// 10-second grace timer (there is no in-flight AI turn to wait for); instead
// it should tear down immediately so the next KeyDown can establish a fresh
// session.
func TestVoiceAgentHoldToTalkReleaseDuringConnectAbortsActivation(t *testing.T) {
	mockAudio := &mockAudioFrameStreamer{}
	mockProvider := newSlowConnectMockLiveProvider()
	session := voiceagent.NewSession(mockProvider, voiceagent.Callbacks{})
	state := &appState{voiceAgentSession: session}

	controller := desktopInputController{
		commands:          &testDesktopCommandBus{},
		recording:         &mutableRecordingState{},
		state:             state,
		voiceAgentSession: session,
		voiceAgentConfig:  &config.VoiceAgentConfig{HoldReleaseGraceSec: 30},
		cfg: &config.Config{
			General: config.GeneralConfig{
				VoiceAgentHotkeyBehavior: config.HotkeyBehaviorHoldToTalk,
			},
			Providers: config.ProvidersConfig{
				Google: config.GoogleProviderConfig{APIKeyEnv: "FAKE_KEY_FOR_HOLD_TO_TALK_ABORT_TEST"},
			},
		},
		audioCapturer: mockAudio,
	}
	// state.appStarted must be true so handleHotkey does not drop the events
	// during the pre-Wails-Run guard window.
	state.mu.Lock()
	state.appStarted = true
	state.mu.Unlock()
	t.Setenv("FAKE_KEY_FOR_HOLD_TO_TALK_ABORT_TEST", "test-api-key")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	controller.handleHotkey(ctx, hotkey.Event{Binding: "voice_agent", Type: hotkey.EventKeyDown})
	waitForCondition(t, 2*time.Second, func() bool { return mockAudio.getHandler() != nil })

	// Connect is still gated; release the hold-to-talk shortcut now. The
	// session must tear down promptly even though the grace period config is
	// 30 seconds — there is no turn to wait for during Connecting.
	controller.handleHotkey(ctx, hotkey.Event{Binding: "voice_agent", Type: hotkey.EventKeyUp})

	// Allow the Connect goroutine to unblock so cleanup can finish. With the
	// activation context already cancelled the provider Connect returns
	// ctx.Err() either way; releasing the gate just avoids leaving a stuck
	// goroutine in the test.
	mockProvider.releaseConnect()

	// session.Start aborts with ctx.Err() on the cancelled activation context,
	// leaves the session in StateInactive, and the activation goroutine runs
	// tearDownVoiceAgentAudioCapture to detach the mic handler. We don't
	// require provider.Close() here — the cancelled Connect short-circuits
	// before a session is ever created, so there is nothing to close.
	waitForCondition(t, 3*time.Second, func() bool {
		return session.CurrentState() == voiceagent.StateInactive && mockAudio.getHandler() == nil
	})

	if state := session.CurrentState(); state != voiceagent.StateInactive {
		t.Fatalf("session state = %s, want %s after release during connect", state, voiceagent.StateInactive)
	}
	if h := mockAudio.getHandler(); h != nil {
		t.Fatal("microphone handler should be detached after release during connect")
	}
}
