package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/hotkey"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
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

type availableSTTProvider struct {
	name string
}

func (p availableSTTProvider) Transcribe(_ context.Context, _ []byte, _ stt.TranscribeOpts) (*stt.Result, error) {
	return &stt.Result{
		Text:     "ok",
		Provider: p.Name(),
	}, nil
}

func (p availableSTTProvider) Name() string {
	if p.name != "" {
		return p.name
	}
	return "cloud"
}

func (p availableSTTProvider) Health(_ context.Context) error {
	return nil
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

func TestDesktopInputControllerPushToTalkKeyDownWhileRecordingDoesNotToggleStop(t *testing.T) {
	bus := &testDesktopCommandBus{}
	controller := desktopInputController{
		commands:  bus,
		recording: testRecordingState{recording: true},
	}

	controller.handlePushToTalk(context.Background(), hotkey.Event{Type: hotkey.EventKeyDown})

	if got := len(bus.commands); got != 0 {
		t.Fatalf("commands = %d, want 0; repeated keydown must not stop hold-to-talk", got)
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
	lastCfg   voiceagent.LiveConfig
	sentAudio int
	audioEnds int
	messages  chan *voiceagent.LiveMessage
}

func newSimpleMockLiveProvider() *simpleMockLiveProvider {
	return &simpleMockLiveProvider{
		messages: make(chan *voiceagent.LiveMessage, 10),
	}
}

func (m *simpleMockLiveProvider) Connect(_ context.Context, cfg voiceagent.LiveConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = true
	m.lastCfg = cfg
	return nil
}

func (m *simpleMockLiveProvider) SendAudio(_ []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentAudio++
	return nil
}

func (m *simpleMockLiveProvider) SendAudioStreamEnd() error {
	m.mu.Lock()
	m.audioEnds++
	m.mu.Unlock()
	m.messages <- &voiceagent.LiveMessage{Done: true}
	return nil
}

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

func (m *simpleMockLiveProvider) configSnapshot() voiceagent.LiveConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastCfg
}

func (m *simpleMockLiveProvider) sendAudioCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sentAudio
}

func (m *simpleMockLiveProvider) audioStreamEndCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.audioEnds
}

func (m *simpleMockLiveProvider) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

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

func TestVoiceAgentMicFramesAreSuppressedDuringAssistantEchoWindow(t *testing.T) {
	mockAudio := &mockAudioFrameStreamer{}
	mockProvider := newSimpleMockLiveProvider()
	session := voiceagent.NewSession(mockProvider, voiceagent.Callbacks{})
	now := time.Unix(100, 0)
	echoGuard := newVoiceAgentEchoGuard(400 * time.Millisecond)
	echoGuard.now = func() time.Time { return now }

	controller := desktopInputController{
		voiceAgentSession: session,
		voiceAgentConfig:  &config.VoiceAgentConfig{},
		cfg: &config.Config{
			Providers: config.ProvidersConfig{
				Google: config.GoogleProviderConfig{APIKeyEnv: "FAKE_KEY_FOR_ECHO_GUARD_TEST"},
			},
		},
		audioCapturer:       mockAudio,
		voiceAgentEchoGuard: echoGuard,
	}

	t.Setenv("FAKE_KEY_FOR_ECHO_GUARD_TEST", "test-api-key")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controller.activateVoiceAgent(ctx)
	time.Sleep(300 * time.Millisecond)

	handler := mockAudio.getHandler()
	if handler == nil {
		t.Fatal("expected microphone handler")
	}

	handler([]byte{1, 0, 1, 0})
	waitForCondition(t, 2*time.Second, func() bool {
		return mockProvider.sendAudioCount() == 1
	})

	echoGuard.markAssistantAudio()
	handler([]byte{2, 0, 2, 0})
	time.Sleep(50 * time.Millisecond)
	if got := mockProvider.sendAudioCount(); got != 1 {
		t.Fatalf("sent audio while echo guard active = %d, want 1", got)
	}

	now = now.Add(401 * time.Millisecond)
	handler([]byte{3, 0, 3, 0})
	waitForCondition(t, 2*time.Second, func() bool {
		return mockProvider.sendAudioCount() == 2
	})

	session.Stop()
}

func TestToggleVoiceAgentPassesFrameworkAndRefinementPromptsToRuntime(t *testing.T) {
	mockAudio := &mockAudioFrameStreamer{}
	mockProvider := newSimpleMockLiveProvider()
	session := voiceagent.NewSession(mockProvider, voiceagent.Callbacks{})

	controller := desktopInputController{
		voiceAgentSession: session,
		voiceAgentConfig: &config.VoiceAgentConfig{
			FrameworkPrompt:  "You are the durable framework prompt.",
			RefinementPrompt: "Address the user by first name.",
		},
		cfg: &config.Config{
			General: config.GeneralConfig{
				Language: "de-DE",
			},
			Vocabulary: config.VocabularyConfig{
				Dictionary: "kombi fire => Kombify",
			},
			Providers: config.ProvidersConfig{
				Google: config.GoogleProviderConfig{APIKeyEnv: "FAKE_KEY_FOR_PROMPT_LAYER_TEST"},
			},
		},
		audioCapturer: mockAudio,
	}

	t.Setenv("FAKE_KEY_FOR_PROMPT_LAYER_TEST", "test-api-key")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	controller.toggleVoiceAgent(ctx)
	time.Sleep(300 * time.Millisecond)

	liveCfg := mockProvider.configSnapshot()
	if got, want := liveCfg.FrameworkPrompt, "You are the durable framework prompt."; got != want {
		t.Fatalf("FrameworkPrompt = %q, want %q", got, want)
	}
	if got, want := liveCfg.RefinementPrompt, "Address the user by first name."; got != want {
		t.Fatalf("RefinementPrompt = %q, want %q", got, want)
	}
	if got, want := liveCfg.Locale, "de-DE"; got != want {
		t.Fatalf("Locale = %q, want %q", got, want)
	}
	if got := liveCfg.VocabularyHint; got == "" {
		t.Fatal("expected vocabulary hint to still be passed into the runtime config")
	}

	session.Stop()
}

func TestDeactivateVoiceAgentClearsMic(t *testing.T) {
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

	controller.deactivateVoiceAgent(ctx, true)

	if session.CurrentState() != voiceagent.StateInactive {
		t.Errorf("expected inactive after deactivation, got %s", session.CurrentState())
	}

	// PCM handler should be cleared.
	if h := mockAudio.getHandler(); h != nil {
		t.Error("expected PCM handler to be nil after deactivation")
	}
}

func TestDeactivateVoiceAgentLogsManualReason(t *testing.T) {
	mockProvider := newSimpleMockLiveProvider()
	session := voiceagent.NewSession(mockProvider, voiceagent.Callbacks{})
	state := &appState{voiceAgentSession: session}
	if err := session.Start(context.Background(), voiceagent.LiveConfig{}, voiceagent.DefaultIdleConfig()); err != nil {
		t.Fatalf("session start failed: %v", err)
	}

	controller := desktopInputController{
		state:             state,
		voiceAgentSession: session,
	}

	controller.deactivateVoiceAgent(context.Background(), true)

	state.mu.Lock()
	logEntries := append([]logEntry(nil), state.logEntries...)
	state.mu.Unlock()
	if len(logEntries) == 0 {
		t.Fatal("expected deactivation log entry")
	}
	if got := logEntries[len(logEntries)-1].Message; !strings.Contains(got, "manual control") {
		t.Fatalf("last log message = %q, want deactivation reason", got)
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

func TestDesktopInputControllerVoiceAgentPipelineFallbackUsesCapturePipeline(t *testing.T) {
	bus := &testDesktopCommandBus{}
	sttRouter := &router.Router{Strategy: router.StrategyCloudOnly}
	sttRouter.AddCloud(availableSTTProvider{name: "ollama"})
	state := &appState{
		agentFlow: fixedAgentFlow(t, "Brainstorming reply"),
		sttRouter: sttRouter,
	}
	controller := desktopInputController{
		commands:  bus,
		recording: testRecordingState{recording: false},
		state:     state,
		sttRouter: sttRouter,
		cfg: &config.Config{
			General: config.GeneralConfig{
				VoiceAgentHotkeyBehavior: config.HotkeyBehaviorPushToTalk,
			},
			Routing: config.RoutingConfig{
				Strategy: "cloud-only",
			},
			VoiceAgent: config.VoiceAgentConfig{
				PipelineFallback: true,
			},
		},
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: modeVoiceAgent,
		Type:    hotkey.EventKeyDown,
	})

	if got := len(bus.commands); got != 2 {
		t.Fatalf("commands = %d, want 2 for voice agent pipeline fallback", got)
	}
	if got, want := bus.commands[0].Type, speechkit.CommandSetActiveMode; got != want {
		t.Fatalf("commands[0].Type = %q, want %q", got, want)
	}
	if got, want := bus.commands[0].Metadata["mode"], modeVoiceAgent; got != want {
		t.Fatalf("commands[0].Metadata[mode] = %q, want %q", got, want)
	}
	if got, want := bus.commands[1].Type, speechkit.CommandStartDictation; got != want {
		t.Fatalf("commands[1].Type = %q, want %q", got, want)
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: modeVoiceAgent,
		Type:    hotkey.EventKeyUp,
	})
	if got := len(bus.commands); got != 2 {
		t.Fatalf("commands after key up = %d, want 2", got)
	}
}

func TestDesktopInputControllerDictationHotkeyBlocksWhenLocalSetupPending(t *testing.T) {
	bus := &testDesktopCommandBus{}
	bubble := &fakeOverlayWindow{}
	state := &appState{assistBubble: bubble}
	controller := desktopInputController{
		commands:  bus,
		recording: &mutableRecordingState{},
		state:     state,
		cfg:       defaultTestConfig(),
		installState: &config.InstallState{
			Mode:      config.InstallModeLocal,
			SetupDone: false,
		},
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: modeDictate,
		Type:    hotkey.EventKeyDown,
	})

	if got := len(bus.commands); got != 0 {
		t.Fatalf("commands = %d, want 0 when local setup is still pending", got)
	}
	state.mu.Lock()
	logEntries := append([]logEntry(nil), state.logEntries...)
	state.mu.Unlock()
	if len(logEntries) == 0 {
		t.Fatal("expected immediate guidance log entry")
	}
	if got := logEntries[len(logEntries)-1].Message; !strings.Contains(got, "local speech model") {
		t.Fatalf("last log message = %q, want local model guidance", got)
	}
	if got := bubble.showCalls; got != 1 {
		t.Fatalf("assist bubble show calls = %d, want 1", got)
	}
	if len(bubble.scripts) == 0 || !strings.Contains(bubble.scripts[len(bubble.scripts)-1], "local speech model") {
		t.Fatalf("bubble scripts = %v, want local model guidance", bubble.scripts)
	}
}

func TestDesktopInputControllerAssistHotkeyBlocksWhenAssistModelMissing(t *testing.T) {
	bus := &testDesktopCommandBus{}
	bubble := &fakeOverlayWindow{}
	sttRouter := &router.Router{Strategy: router.StrategyDynamic}
	sttRouter.AddCloud(availableSTTProvider{name: "openai"})
	state := &appState{
		assistBubble: bubble,
		sttRouter:    sttRouter,
	}
	controller := desktopInputController{
		commands:  bus,
		recording: &mutableRecordingState{},
		state:     state,
		cfg:       defaultTestConfig(),
		installState: &config.InstallState{
			Mode:      config.InstallModeLocal,
			SetupDone: true,
		},
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: modeAssist,
		Type:    hotkey.EventKeyDown,
	})

	if got := len(bus.commands); got != 0 {
		t.Fatalf("commands = %d, want 0 when Assist has no model configured", got)
	}
	state.mu.Lock()
	logEntries := append([]logEntry(nil), state.logEntries...)
	state.mu.Unlock()
	if len(logEntries) == 0 {
		t.Fatal("expected immediate Assist guidance log entry")
	}
	if got := logEntries[len(logEntries)-1].Message; !strings.Contains(got, "Assist model") {
		t.Fatalf("last log message = %q, want Assist model guidance", got)
	}
	if got := bubble.showCalls; got != 1 {
		t.Fatalf("assist bubble show calls = %d, want 1", got)
	}
	if len(bubble.scripts) == 0 || !strings.Contains(bubble.scripts[len(bubble.scripts)-1], "Assist model") {
		t.Fatalf("bubble scripts = %v, want Assist model guidance", bubble.scripts)
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

func TestDesktopInputControllerVoiceAgentHotkeyToggleDispatchesOnlyActiveModeOnStart(t *testing.T) {
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
				AgentMode:                "voice_agent",
				VoiceAgentHotkeyBehavior: config.HotkeyBehaviorToggle,
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

	if got := len(bus.commands); got != 1 {
		t.Fatalf("commands = %d, want 1 while voice agent activates", got)
	}
	if got, want := bus.commands[0].Type, speechkit.CommandSetActiveMode; got != want {
		t.Fatalf("commands[0].Type = %q, want %q", got, want)
	}
	if got, want := bus.commands[0].Metadata["mode"], modeVoiceAgent; got != want {
		t.Fatalf("commands[0].Metadata[mode] = %q, want %q", got, want)
	}
	if session.CurrentState() == voiceagent.StateInactive {
		t.Fatal("expected voice agent to be active after first key down")
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "agent",
		Type:    hotkey.EventKeyUp,
	})
	if got := len(bus.commands); got != 1 {
		t.Fatalf("commands = %d, want 1 after voice agent key up", got)
	}
	if session.CurrentState() == voiceagent.StateInactive {
		t.Fatal("expected voice agent to stay active after key up")
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "agent",
		Type:    hotkey.EventKeyDown,
	})

	if got := len(bus.commands); got != 1 {
		t.Fatalf("commands = %d, want 1 after second voice agent key down", got)
	}
	if got := session.CurrentState(); got == voiceagent.StateInactive {
		t.Fatal("expected voice agent to stay active after second key down")
	}
	if h := mockAudio.getHandler(); h == nil {
		t.Fatal("expected microphone handler to remain attached after second key down")
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

func TestDesktopInputControllerAssistToggleModeStartsAndStopsOnKeyDownOnly(t *testing.T) {
	bus := &testDesktopCommandBus{}
	recording := &mutableRecordingState{}
	state := &appState{}
	controller := desktopInputController{
		commands:  bus,
		recording: recording,
		state:     state,
		cfg: &config.Config{
			General: config.GeneralConfig{
				AssistHotkeyBehavior: config.HotkeyBehaviorToggle,
			},
		},
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
	if got, want := bus.commands[1].Type, speechkit.CommandStartDictation; got != want {
		t.Fatalf("commands[1].Type = %q, want %q", got, want)
	}

	recording.recording = true
	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "assist",
		Type:    hotkey.EventKeyUp,
	})

	if got := len(bus.commands); got != 2 {
		t.Fatalf("commands after key up = %d, want 2", got)
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "assist",
		Type:    hotkey.EventKeyDown,
	})

	if got := len(bus.commands); got != 4 {
		t.Fatalf("commands after second key down = %d, want 4", got)
	}
	if got, want := bus.commands[3].Type, speechkit.CommandStopDictation; got != want {
		t.Fatalf("commands[3].Type = %q, want %q", got, want)
	}
}

func TestDesktopInputControllerVoiceAgentBindingToggleDispatchesOnlyActiveModeOnStart(t *testing.T) {
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
				VoiceAgentHotkeyBehavior: config.HotkeyBehaviorToggle,
			},
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

	if got := len(bus.commands); got != 1 {
		t.Fatalf("commands = %d, want 1 while voice agent activates", got)
	}
	if got, want := bus.commands[0].Type, speechkit.CommandSetActiveMode; got != want {
		t.Fatalf("commands[0].Type = %q, want %q", got, want)
	}
	if got, want := bus.commands[0].Metadata["mode"], modeVoiceAgent; got != want {
		t.Fatalf("commands[0].Metadata[mode] = %q, want %q", got, want)
	}
	if session.CurrentState() == voiceagent.StateInactive {
		t.Fatal("expected voice agent to be active after key down")
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "voice_agent",
		Type:    hotkey.EventKeyUp,
	})

	if got := len(bus.commands); got != 1 {
		t.Fatalf("commands = %d, want 1 after voice agent key up", got)
	}
	if session.CurrentState() == voiceagent.StateInactive {
		t.Fatal("expected voice agent to stay active after key up")
	}
}

func TestDesktopInputControllerCloseVoiceAgentPrompterHidesWhenConfiguredToContinue(t *testing.T) {
	prompter := &fakeOverlayWindow{visible: true}
	state := &appState{prompterWindow: prompter}
	controller := desktopInputController{
		state: state,
		cfg: &config.Config{
			VoiceAgent: config.VoiceAgentConfig{
				CloseBehavior: config.VoiceAgentCloseBehaviorContinue,
			},
		},
	}

	controller.closeVoiceAgentPrompter(context.Background())

	if got := prompter.hideCalls; got != 1 {
		t.Fatalf("prompter hide calls = %d, want 1", got)
	}
	if got := prompter.minimiseCalls; got != 0 {
		t.Fatalf("prompter minimise calls = %d, want 0", got)
	}
}

func TestDesktopInputControllerCloseVoiceAgentPrompterEndsChatWhenConfiguredForNewChat(t *testing.T) {
	prompter := &fakeOverlayWindow{visible: true}
	mockProvider := newSimpleMockLiveProvider()
	session := voiceagent.NewSession(mockProvider, voiceagent.Callbacks{})
	t.Setenv("FAKE_KEY_FOR_VOICE_AGENT_CLOSE_TEST", "test-api-key")
	state := &appState{prompterWindow: prompter}
	controller := desktopInputController{
		state:             state,
		voiceAgentSession: session,
		cfg: &config.Config{
			Providers: config.ProvidersConfig{
				Google: config.GoogleProviderConfig{APIKeyEnv: "FAKE_KEY_FOR_VOICE_AGENT_CLOSE_TEST"},
			},
			VoiceAgent: config.VoiceAgentConfig{
				CloseBehavior: config.VoiceAgentCloseBehaviorNewChat,
			},
		},
	}

	controller.activateVoiceAgent(context.Background())
	time.Sleep(300 * time.Millisecond)

	controller.closeVoiceAgentPrompter(context.Background())

	if got := session.CurrentState(); got != voiceagent.StateInactive {
		t.Fatalf("session state = %s, want %s after close", got, voiceagent.StateInactive)
	}
	if got := prompter.hideCalls; got != 1 {
		t.Fatalf("prompter hide calls = %d, want 1", got)
	}
	if got := prompter.minimiseCalls; got != 0 {
		t.Fatalf("prompter minimise calls = %d, want 0", got)
	}
	if got := len(prompter.scripts); got < 2 {
		t.Fatalf("prompter scripts = %d, want clear/update JS calls", got)
	}
}

func TestDesktopInputControllerVoiceAgentPushToTalkEndsRealtimeSessionOnKeyUp(t *testing.T) {
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
				VoiceAgentHotkeyBehavior: config.HotkeyBehaviorPushToTalk,
			},
			Providers: config.ProvidersConfig{
				Google: config.GoogleProviderConfig{APIKeyEnv: "FAKE_KEY_FOR_VOICE_AGENT_PTT_TEST"},
			},
		},
		audioCapturer: mockAudio,
	}

	t.Setenv("FAKE_KEY_FOR_VOICE_AGENT_PTT_TEST", "test-api-key")

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "voice_agent",
		Type:    hotkey.EventKeyDown,
	})
	time.Sleep(300 * time.Millisecond)

	if got := len(bus.commands); got != 1 {
		t.Fatalf("commands after key down = %d, want 1", got)
	}
	if got, want := bus.commands[0].Type, speechkit.CommandSetActiveMode; got != want {
		t.Fatalf("commands[0].Type = %q, want %q", got, want)
	}
	if got, want := bus.commands[0].Metadata["mode"], modeVoiceAgent; got != want {
		t.Fatalf("commands[0].Metadata[mode] = %q, want %q", got, want)
	}
	if got := session.CurrentState(); got == voiceagent.StateInactive {
		t.Fatal("expected voice agent to be active after key down")
	}

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "voice_agent",
		Type:    hotkey.EventKeyUp,
	})

	if got := len(bus.commands); got != 1 {
		t.Fatalf("commands after key up = %d, want 1", got)
	}
	if got := mockProvider.audioStreamEndCount(); got != 1 {
		t.Fatalf("audio stream end count = %d, want 1", got)
	}
	if h := mockAudio.getHandler(); h != nil {
		t.Fatal("expected microphone handler to be detached after push-to-talk key up")
	}
	waitForCondition(t, 2*time.Second, func() bool {
		return session.CurrentState() == voiceagent.StateInactive && mockProvider.isClosed()
	})
}

func TestDesktopInputControllerVoiceAgentPushToTalkShowsReadyPrompterWithoutSecondClick(t *testing.T) {
	bus := &testDesktopCommandBus{}
	prompter := &fakeOverlayWindow{}
	mockAudio := &mockAudioFrameStreamer{}
	mockProvider := newSimpleMockLiveProvider()
	state := &appState{
		prompterWindow: prompter,
	}
	session := voiceagent.NewSession(mockProvider, voiceagent.Callbacks{
		OnStateChange: func(vaState voiceagent.State) {
			state.updatePrompterState(string(vaState))
		},
	})
	state.voiceAgentSession = session
	controller := desktopInputController{
		commands:          bus,
		recording:         &mutableRecordingState{},
		state:             state,
		voiceAgentSession: session,
		voiceAgentConfig: &config.VoiceAgentConfig{
			ShowPrompter: true,
		},
		cfg: &config.Config{
			General: config.GeneralConfig{
				VoiceAgentHotkeyBehavior: config.HotkeyBehaviorPushToTalk,
			},
			Providers: config.ProvidersConfig{
				Google: config.GoogleProviderConfig{APIKeyEnv: "FAKE_KEY_FOR_VOICE_AGENT_READY_PROMPTER_TEST"},
			},
		},
		audioCapturer: mockAudio,
	}

	t.Setenv("FAKE_KEY_FOR_VOICE_AGENT_READY_PROMPTER_TEST", "test-api-key")

	controller.handleHotkey(context.Background(), hotkey.Event{
		Binding: "voice_agent",
		Type:    hotkey.EventKeyDown,
	})
	waitForCondition(t, 2*time.Second, func() bool {
		return prompter.showCalls > 0 && strings.Contains(strings.Join(prompter.scripts, "\n"), `updateState("connecting")`)
	})

	if got := prompter.showCalls; got == 0 {
		t.Fatal("prompter should show on voice agent hotkey key down")
	}
	combinedScripts := strings.Join(prompter.scripts, "\n")
	if !strings.Contains(combinedScripts, `setMode("voice_agent")`) {
		t.Fatalf("prompter scripts missing voice agent mode switch: %s", combinedScripts)
	}
	if !strings.Contains(combinedScripts, `updateState("connecting")`) {
		t.Fatalf("prompter scripts missing immediate active voice agent state: %s", combinedScripts)
	}

	session.Stop()
}

func TestMaybeAutoStartVoiceAgentOnLaunchActivatesSession(t *testing.T) {
	mockAudio := &mockAudioFrameStreamer{}
	mockProvider := newSimpleMockLiveProvider()
	session := voiceagent.NewSession(mockProvider, voiceagent.Callbacks{})
	controller := &desktopInputController{
		voiceAgentSession: session,
		voiceAgentConfig: &config.VoiceAgentConfig{
			AutoStartOnLaunch: true,
		},
		cfg: &config.Config{
			General: config.GeneralConfig{
				VoiceAgentEnabled: true,
				VoiceAgentHotkey:  "ctrl+shift",
				AutoStartOnLaunch: true,
			},
			Providers: config.ProvidersConfig{
				Google: config.GoogleProviderConfig{APIKeyEnv: "FAKE_KEY_FOR_VOICE_AGENT_AUTOSTART_TEST"},
			},
			VoiceAgent: config.VoiceAgentConfig{
				Enabled:           true,
				AutoStartOnLaunch: true,
			},
		},
		audioCapturer: mockAudio,
	}

	t.Setenv("FAKE_KEY_FOR_VOICE_AGENT_AUTOSTART_TEST", "test-api-key")

	maybeAutoStartVoiceAgentOnLaunch(context.Background(), controller.cfg, controller)
	time.Sleep(300 * time.Millisecond)

	if got := session.CurrentState(); got == voiceagent.StateInactive {
		t.Fatal("expected voice agent to auto-start on launch")
	}
}

func TestMaybeAutoStartVoiceAgentOnLaunchSkipsWhenDisabled(t *testing.T) {
	mockAudio := &mockAudioFrameStreamer{}
	mockProvider := newSimpleMockLiveProvider()
	session := voiceagent.NewSession(mockProvider, voiceagent.Callbacks{})
	controller := &desktopInputController{
		voiceAgentSession: session,
		voiceAgentConfig:  &config.VoiceAgentConfig{},
		cfg: &config.Config{
			General: config.GeneralConfig{
				VoiceAgentEnabled: true,
				VoiceAgentHotkey:  "ctrl+shift",
				AutoStartOnLaunch: false,
			},
			Providers: config.ProvidersConfig{
				Google: config.GoogleProviderConfig{APIKeyEnv: "FAKE_KEY_FOR_VOICE_AGENT_AUTOSTART_SKIP_TEST"},
			},
			VoiceAgent: config.VoiceAgentConfig{
				Enabled:           true,
				AutoStartOnLaunch: false,
			},
		},
		audioCapturer: mockAudio,
	}

	t.Setenv("FAKE_KEY_FOR_VOICE_AGENT_AUTOSTART_SKIP_TEST", "test-api-key")

	maybeAutoStartVoiceAgentOnLaunch(context.Background(), controller.cfg, controller)
	time.Sleep(50 * time.Millisecond)

	if got := session.CurrentState(); got != voiceagent.StateInactive {
		t.Fatalf("session state = %s, want inactive when auto-start is disabled", got)
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, predicate func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("condition not met within %s", timeout)
}
