package speechkit

import (
	"context"
	"errors"
	"testing"
)

func TestRuntimeStateReturnsClone(t *testing.T) {
	runtime := NewRuntime(Snapshot{
		Status:    "idle",
		Providers: []string{"local", "hf"},
	}, Hooks{})

	snapshot := runtime.State()
	snapshot.Providers[0] = "mutated"

	current := runtime.State()
	if got, want := current.Providers[0], "local"; got != want {
		t.Fatalf("providers[0] = %q, want %q", got, want)
	}
}

func TestRuntimeUpdateStateClonesProviders(t *testing.T) {
	runtime := NewRuntime(Snapshot{}, Hooks{})

	providers := []string{"local"}
	runtime.UpdateState(func(snapshot *Snapshot) {
		snapshot.Providers = providers
	})
	providers[0] = "mutated"

	current := runtime.State()
	if got, want := current.Providers[0], "local"; got != want {
		t.Fatalf("providers[0] = %q, want %q", got, want)
	}
}

func TestRuntimePublishAddsTimestamp(t *testing.T) {
	runtime := NewRuntime(Snapshot{}, Hooks{})
	defer runtime.Close()

	if ok := runtime.Publish(Event{Type: EventRecordingStarted, Message: "recording"}); !ok {
		t.Fatal("Publish() = false, want true")
	}

	event := <-runtime.Events()
	if event.Time.IsZero() {
		t.Fatal("event.Time is zero")
	}
	if got, want := event.Type, EventRecordingStarted; got != want {
		t.Fatalf("event.Type = %q, want %q", got, want)
	}
}

func TestRuntimeCommandsDispatchClone(t *testing.T) {
	var received Command
	runtime := NewRuntime(Snapshot{}, Hooks{
		HandleCommand: func(_ context.Context, command Command) error {
			received = command
			return nil
		},
	})

	command := Command{
		Type:     CommandShowDashboard,
		Metadata: map[string]string{"source": "test"},
	}
	if err := runtime.Commands().Dispatch(context.Background(), command); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	command.Metadata["source"] = "mutated"
	if got, want := received.Metadata["source"], "test"; got != want {
		t.Fatalf("received.Metadata[source] = %q, want %q", got, want)
	}
}

func TestRuntimeCommandsWithoutHandler(t *testing.T) {
	runtime := NewRuntime(Snapshot{}, Hooks{})

	err := runtime.Commands().Dispatch(context.Background(), Command{Type: CommandShowDashboard})
	if !errors.Is(err, ErrCommandHandlerUnavailable) {
		t.Fatalf("Dispatch() error = %v, want %v", err, ErrCommandHandlerUnavailable)
	}
}

func TestRuntimeStartStopHooks(t *testing.T) {
	started := false
	stopped := false
	runtime := NewRuntime(Snapshot{}, Hooks{
		Start: func(context.Context) error {
			started = true
			return nil
		},
		Stop: func(context.Context) error {
			stopped = true
			return nil
		},
	})

	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !started {
		t.Fatal("start hook was not called")
	}
	if !stopped {
		t.Fatal("stop hook was not called")
	}
}

func TestRuntimeStateClonesQuickCaptureFlags(t *testing.T) {
	runtime := NewRuntime(Snapshot{
		Status:           "idle",
		QuickNoteMode:    true,
		QuickCaptureMode: true,
	}, Hooks{})

	snapshot := runtime.State()
	snapshot.QuickNoteMode = false
	snapshot.QuickCaptureMode = false

	current := runtime.State()
	if !current.QuickNoteMode {
		t.Fatal("QuickNoteMode = false, want true")
	}
	if !current.QuickCaptureMode {
		t.Fatal("QuickCaptureMode = false, want true")
	}
}
