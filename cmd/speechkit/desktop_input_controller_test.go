package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/hotkey"
	"github.com/kombifyio/SpeechKit/internal/voiceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type testDesktopCommandBus struct {
	commands []speechkit.Command
}

func (b *testDesktopCommandBus) Dispatch(_ context.Context, command speechkit.Command) error {
	b.commands = append(b.commands, command.Clone())
	return nil
}

type testRecordingState struct {
	recording bool
}

func (r testRecordingState) IsRecording() bool {
	return r.recording
}

type mutableRecordingState struct {
	recording bool
}

func (r *mutableRecordingState) IsRecording() bool {
	return r.recording
}

func TestAppStateArmQuickCaptureSetsRuntimeCaptureMode(t *testing.T) {
	state := &appState{}

	state.armQuickCapture(42)

	runtime := state.runtimeStateLocked()
	if !runtime.quickCaptureMode {
		t.Fatal("runtime.quickCaptureMode = false, want true")
	}
	if !runtime.quickCaptureAutoStart {
		t.Fatal("runtime.quickCaptureAutoStart = false, want true")
	}
	if got, want := runtime.quickCaptureNoteID, int64(42); got != want {
		t.Fatalf("runtime.quickCaptureNoteID = %d, want %d", got, want)
	}

	snapshot := state.speechkitSnapshotLocked()
	if !snapshot.QuickCaptureMode {
		t.Fatal("snapshot.QuickCaptureMode = false, want true")
	}
}

func TestDesktopInputControllerHotkeyKeyUpIgnoresQuickCaptureMode(t *testing.T) {
	state := &appState{}
	state.armQuickCapture(42)
	bus := &testDesktopCommandBus{}
	controller := desktopInputController{
		commands:  bus,
		recording: testRecordingState{recording: true},
		state:     state,
	}

	controller.handleHotkey(context.Background(), hotkey.Event{Type: hotkey.EventKeyUp})

	if got := len(bus.commands); got != 0 {
		t.Fatalf("commands = %d, want 0", got)
	}
}

func TestDesktopInputControllerAutoStartConsumesPendingFlag(t *testing.T) {
	state := &appState{}
	state.armQuickCapture(7)
	bus := &testDesktopCommandBus{}
	controller := desktopInputController{
		commands:  bus,
		recording: testRecordingState{recording: false},
		state:     state,
	}

	controller.handleAutoStartTick(context.Background())

	if got := len(bus.commands); got != 1 {
		t.Fatalf("commands = %d, want 1", got)
	}
	if got, want := bus.commands[0].Type, speechkit.CommandStartDictation; got != want {
		t.Fatalf("commands[0].Type = %q, want %q", got, want)
	}

	runtime := state.runtimeStateLocked()
	if runtime.quickCaptureAutoStart {
		t.Fatal("runtime.quickCaptureAutoStart = true, want false")
	}
	if !runtime.quickCaptureMode {
		t.Fatal("runtime.quickCaptureMode = false, want true")
	}
}

func TestDesktopInputControllerRunStopsOnSilence(t *testing.T) {
	state := &appState{}
	state.armQuickCapture(9)
	bus := &testDesktopCommandBus{}
	silence := make(chan struct{}, 1)
	controller := desktopInputController{
		commands:          bus,
		recording:         testRecordingState{recording: true},
		state:             state,
		silenceAutoStop:   silence,
		autoStartInterval: time.Hour,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		controller.Run(ctx)
		close(done)
	}()

	silence <- struct{}{}
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	if got := len(bus.commands); got != 1 {
		t.Fatalf("commands = %d, want 1", got)
	}
	if got, want := bus.commands[0].Type, speechkit.CommandStopDictation; got != want {
		t.Fatalf("commands[0].Type = %q, want %q", got, want)
	}
}

// mockAudioFrameStreamer records SetPCMHandler and Start calls.
type mockAudioFrameStreamer struct {
	mu         sync.Mutex
	handler    func([]byte)
	started    bool
	startCount int
}

func (m *mockAudioFrameStreamer) SetPCMHandler(fn func([]byte)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handler = fn
}

func (m *mockAudioFrameStreamer) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	m.startCount++
	return nil
}

func (m *mockAudioFrameStreamer) getHandler() func([]byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.handler
}

// simpleMockLiveProvider implements voiceagent.LiveProvider for controller tests.
type simpleMockLiveProvider struct {
	mu        sync.Mutex
	connected bool
	closed    bool
	messages  chan *voiceagent.LiveMessage
}

func newSimpleMockLiveProvider() *simpleMockLiveProvider {
	return &simpleMockLiveProvider{
		messages: make(chan *voiceagent.LiveMessage, 10),
	}
}

func (m *simpleMockLiveProvider) Connect(_ context.Context, _ voiceagent.LiveConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = true
	return nil
}

func (m *simpleMockLiveProvider) SendAudio(_ []byte) error { return nil }

func (m *simpleMockLiveProvider) Receive(ctx context.Context) (*voiceagent.LiveMessage, error) {
	select {
	case msg := <-m.messages:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *simpleMockLiveProvider) SendText(_ string) error { return nil }

func (m *simpleMockLiveProvider) SendToolResponse(_ voiceagent.ToolResponse) error { return nil }

func (m *simpleMockLiveProvider) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *simpleMockLiveProvider) Name() string { return "simple-mock" }

func TestToggleVoiceAgentActivatesAndWiresMic(t *testing.T) {
	mockAudio := &mockAudioFrameStreamer{}
	mockProvider := newSimpleMockLiveProvider()
	session := voiceagent.NewSession(mockProvider, voiceagent.Callbacks{})

	controller := desktopInputController{
		voiceAgentSession: session,
		voiceAgentConfig:  &config.VoiceAgentConfig{},
		cfg: &config.Config{
			Providers: config.ProvidersConfig{
				Google: config.GoogleProviderConfig{APIKeyEnv: "FAKE_KEY_FOR_TEST"},
			},
		},
		audioCapturer: mockAudio,
		// state is nil: skip startVoiceAgentStream (oto init) in test.
	}

	// Set the env var so ResolveSecret finds it.
	t.Setenv("FAKE_KEY_FOR_TEST", "test-api-key")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	controller.toggleVoiceAgent(ctx)

	// Wait for the goroutine to finish starting.
	time.Sleep(300 * time.Millisecond)

	if session.CurrentState() == voiceagent.StateInactive {
		t.Fatal("expected voice agent to be active")
	}

	mockAudio.mu.Lock()
	if !mockAudio.started {
		t.Error("expected audio capturer Start() to be called")
	}
	if mockAudio.handler == nil {
		t.Error("expected PCM handler to be set on audio capturer")
	}
	mockAudio.mu.Unlock()

	session.Stop()
}

func TestToggleVoiceAgentDeactivateClearsMic(t *testing.T) {
	mockAudio := &mockAudioFrameStreamer{}
	mockProvider := newSimpleMockLiveProvider()
	session := voiceagent.NewSession(mockProvider, voiceagent.Callbacks{})

	controller := desktopInputController{
		voiceAgentSession: session,
		voiceAgentConfig:  &config.VoiceAgentConfig{},
		cfg: &config.Config{
			Providers: config.ProvidersConfig{
				Google: config.GoogleProviderConfig{APIKeyEnv: "FAKE_KEY_FOR_TEST2"},
			},
		},
		audioCapturer: mockAudio,
		// state is nil: skip startVoiceAgentStream (oto init) in test.
	}

	t.Setenv("FAKE_KEY_FOR_TEST2", "test-api-key-2")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// First toggle: activate.
	controller.toggleVoiceAgent(ctx)
	time.Sleep(300 * time.Millisecond)

	if session.CurrentState() == voiceagent.StateInactive {
		t.Fatal("expected voice agent to be active before deactivation")
	}

	// Second toggle: deactivate.
	controller.toggleVoiceAgent(ctx)

	if session.CurrentState() != voiceagent.StateInactive {
		t.Errorf("expected inactive after deactivation, got %s", session.CurrentState())
	}

	// PCM handler should be cleared.
	if h := mockAudio.getHandler(); h != nil {
		t.Error("expected PCM handler to be nil after deactivation")
	}
}

func TestDesktopInputControllerVoiceAgentKeyUpDoesNotDispatchPTTCommands(t *testing.T) {
	bus := &testDesktopCommandBus{}
	controller := desktopInputController{
		commands:  bus,
		recording: testRecordingState{recording: true},
		cfg: &config.Config{
			General: config.GeneralConfig{
				AgentMode: "voice_agent",
			},
		},
		voiceAgentSession: voiceagent.NewSession(newSimpleMockLiveProvider(), voiceagent.Callbacks{}),
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "agent",
		Type:    hotkey.EventKeyUp,
	})

	if got := len(bus.commands); got != 0 {
		t.Fatalf("commands = %d, want 0 for voice agent key up", got)
	}
}

func TestDesktopInputControllerAssistHotkeyStartsDictationAndLogsRoute(t *testing.T) {
	bus := &testDesktopCommandBus{}
	recording := &mutableRecordingState{}
	state := &appState{}
	controller := desktopInputController{
		commands:  bus,
		recording: recording,
		state:     state,
		cfg: &config.Config{
			General: config.GeneralConfig{
				AgentMode: "assist",
			},
		},
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "agent",
		Type:    hotkey.EventKeyDown,
	})

	if got := len(bus.commands); got != 2 {
		t.Fatalf("commands = %d, want 2", got)
	}
	if got, want := bus.commands[0].Type, speechkit.CommandSetActiveMode; got != want {
		t.Fatalf("commands[0].Type = %q, want %q", got, want)
	}
	if got, want := bus.commands[0].Metadata["mode"], "assist"; got != want {
		t.Fatalf("commands[0].Metadata[mode] = %q, want %q", got, want)
	}
	if got, want := bus.commands[1].Type, speechkit.CommandStartDictation; got != want {
		t.Fatalf("commands[1].Type = %q, want %q", got, want)
	}

	state.mu.Lock()
	logEntries := append([]logEntry(nil), state.logEntries...)
	state.mu.Unlock()
	if len(logEntries) == 0 {
		t.Fatal("expected assist route log entry")
	}
	if got, want := logEntries[len(logEntries)-1].Message, "Agent hotkey routed to Assist capture"; got != want {
		t.Fatalf("last log message = %q, want %q", got, want)
	}

	recording.recording = true
	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "agent",
		Type:    hotkey.EventKeyUp,
	})

	if got := len(bus.commands); got != 3 {
		t.Fatalf("commands = %d, want 3 after key up", got)
	}
	if got, want := bus.commands[2].Type, speechkit.CommandStopDictation; got != want {
		t.Fatalf("commands[2].Type = %q, want %q", got, want)
	}
}

func TestDesktopInputControllerVoiceAgentHotkeyTogglesWithoutDispatchingDictation(t *testing.T) {
	bus := &testDesktopCommandBus{}
	mockAudio := &mockAudioFrameStreamer{}
	mockProvider := newSimpleMockLiveProvider()
	session := voiceagent.NewSession(mockProvider, voiceagent.Callbacks{})
	controller := desktopInputController{
		commands:          bus,
		recording:         &mutableRecordingState{},
		voiceAgentSession: session,
		voiceAgentConfig:  &config.VoiceAgentConfig{},
		cfg: &config.Config{
			General: config.GeneralConfig{
				AgentMode: "voice_agent",
			},
			Providers: config.ProvidersConfig{
				Google: config.GoogleProviderConfig{APIKeyEnv: "FAKE_KEY_FOR_HOTKEY_TOGGLE_TEST"},
			},
		},
		audioCapturer: mockAudio,
	}

	t.Setenv("FAKE_KEY_FOR_HOTKEY_TOGGLE_TEST", "test-api-key")

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "agent",
		Type:    hotkey.EventKeyDown,
	})
	time.Sleep(300 * time.Millisecond)

	if got := len(bus.commands); got != 0 {
		t.Fatalf("commands = %d, want 0 while voice agent activates", got)
	}
	if session.CurrentState() == voiceagent.StateInactive {
		t.Fatal("expected voice agent to be active after first key down")
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "agent",
		Type:    hotkey.EventKeyUp,
	})
	if got := len(bus.commands); got != 0 {
		t.Fatalf("commands = %d, want 0 after voice agent key up", got)
	}
	if session.CurrentState() == voiceagent.StateInactive {
		t.Fatal("expected voice agent to stay active after key up")
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "agent",
		Type:    hotkey.EventKeyDown,
	})

	if got := len(bus.commands); got != 0 {
		t.Fatalf("commands = %d, want 0 after second voice agent key down", got)
	}
	if got, want := session.CurrentState(), voiceagent.StateInactive; got != want {
		t.Fatalf("session state = %s, want %s after second key down", got, want)
	}
}

func TestDesktopInputControllerAssistBindingSetsAssistModeAndUsesPTT(t *testing.T) {
	bus := &testDesktopCommandBus{}
	recording := &mutableRecordingState{}
	state := &appState{}
	controller := desktopInputController{
		commands:  bus,
		recording: recording,
		state:     state,
		cfg:       defaultTestConfig(),
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "assist",
		Type:    hotkey.EventKeyDown,
	})

	if got := len(bus.commands); got != 2 {
		t.Fatalf("commands = %d, want 2", got)
	}
	if got, want := bus.commands[0].Type, speechkit.CommandSetActiveMode; got != want {
		t.Fatalf("commands[0].Type = %q, want %q", got, want)
	}
	if got, want := bus.commands[0].Metadata["mode"], "assist"; got != want {
		t.Fatalf("commands[0].Metadata[mode] = %q, want %q", got, want)
	}
	if got, want := bus.commands[1].Type, speechkit.CommandStartDictation; got != want {
		t.Fatalf("commands[1].Type = %q, want %q", got, want)
	}

	recording.recording = true
	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "assist",
		Type:    hotkey.EventKeyUp,
	})

	if got := len(bus.commands); got != 3 {
		t.Fatalf("commands = %d, want 3 after key up", got)
	}
	if got, want := bus.commands[2].Type, speechkit.CommandStopDictation; got != want {
		t.Fatalf("commands[2].Type = %q, want %q", got, want)
	}
}

func TestDesktopInputControllerVoiceAgentBindingTogglesWithoutPTT(t *testing.T) {
	bus := &testDesktopCommandBus{}
	mockAudio := &mockAudioFrameStreamer{}
	mockProvider := newSimpleMockLiveProvider()
	session := voiceagent.NewSession(mockProvider, voiceagent.Callbacks{})
	controller := desktopInputController{
		commands:          bus,
		recording:         &mutableRecordingState{},
		voiceAgentSession: session,
		voiceAgentConfig:  &config.VoiceAgentConfig{},
		cfg: &config.Config{
			Providers: config.ProvidersConfig{
				Google: config.GoogleProviderConfig{APIKeyEnv: "FAKE_KEY_FOR_SEPARATE_VOICE_AGENT_TEST"},
			},
		},
		audioCapturer: mockAudio,
	}

	t.Setenv("FAKE_KEY_FOR_SEPARATE_VOICE_AGENT_TEST", "test-api-key")

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "voice_agent",
		Type:    hotkey.EventKeyDown,
	})
	time.Sleep(300 * time.Millisecond)

	if got := len(bus.commands); got != 0 {
		t.Fatalf("commands = %d, want 0 while voice agent activates", got)
	}
	if session.CurrentState() == voiceagent.StateInactive {
		t.Fatal("expected voice agent to be active after key down")
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "voice_agent",
		Type:    hotkey.EventKeyUp,
	})

	if got := len(bus.commands); got != 0 {
		t.Fatalf("commands = %d, want 0 after voice agent key up", got)
	}
	if session.CurrentState() == voiceagent.StateInactive {
		t.Fatal("expected voice agent to stay active after key up")
	}
}
