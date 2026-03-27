package main

import (
	"context"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/hotkey"
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
