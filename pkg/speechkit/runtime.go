// Package speechkit provides the public SDK for embedding SpeechKit voice
// capture and transcription into host applications.
//
// The central type is [Runtime], which manages shared state and event delivery.
// An [Engine] is the full voice pipeline; [RecordingController] and
// [TranscriptionWorker] can be composed independently for custom pipelines.
package speechkit

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrCommandHandlerUnavailable is returned by [CommandBus.Dispatch] when no
// command handler has been configured on the [Runtime].
var ErrCommandHandlerUnavailable = errors.New("speechkit: no command handler configured")

// EventType identifies the kind of event published to the event channel.
type EventType string

const (
	EventStateChanged        EventType = "state.changed"
	EventRecordingStarted    EventType = "recording.started"
	EventProcessingStarted   EventType = "processing.started"
	EventTranscriptionReady  EventType = "transcription.ready"
	EventTranscriptCommitted EventType = "transcription.committed"
	EventQuickNoteModeArmed  EventType = "quicknote.mode_armed"
	EventQuickNoteUpdated    EventType = "quicknote.updated"
	EventWarningRaised       EventType = "warning.raised"
	EventErrorRaised         EventType = "error.raised"
	EventShortcutMatched     EventType = "shortcut.matched"
)

// Event is a notification published to the event channel returned by
// [Runtime.Events]. Consumers should switch on Type and inspect the
// relevant fields.
type Event struct {
	Type      EventType
	Time      time.Time
	Message   string
	Text      string
	Provider  string
	QuickNote bool
	Err       error
	Shortcut  string
}

// Snapshot is a point-in-time copy of the Runtime's observable state.
// All slice and map fields are safe to read without holding any lock.
type Snapshot struct {
	Status                string
	Text                  string
	Level                 float64
	Hotkey                string
	ActiveMode            string
	Providers             []string
	ActiveProfiles        map[string]string
	Transcriptions        int
	QuickNoteMode         bool
	QuickCaptureMode      bool
	LastTranscriptionText string
}

func (s Snapshot) Clone() Snapshot {
	clone := s
	if s.Providers != nil {
		clone.Providers = append([]string(nil), s.Providers...)
	}
	if s.ActiveProfiles != nil {
		clone.ActiveProfiles = make(map[string]string, len(s.ActiveProfiles))
		for key, value := range s.ActiveProfiles {
			clone.ActiveProfiles[key] = value
		}
	}
	return clone
}

// CommandType identifies the action a [Command] requests.
type CommandType string

const (
	CommandShowDashboard           CommandType = "dashboard.show"
	CommandStartDictation          CommandType = "dictation.start"
	CommandStopDictation           CommandType = "dictation.stop"
	CommandSetActiveMode           CommandType = "mode.set_active"
	CommandOpenQuickNote           CommandType = "quicknote.open"
	CommandOpenQuickCapture        CommandType = "quicknote.capture.open"
	CommandCloseQuickCapture       CommandType = "quicknote.capture.close"
	CommandArmQuickNoteRecording   CommandType = "quicknote.record.arm"
	CommandCopyLastTranscription   CommandType = "transcription.copy_last"
	CommandInsertLastTranscription CommandType = "transcription.insert_last"
	CommandSummarizeSelection      CommandType = "selection.summarize"
)

// Command is a request dispatched through the [CommandBus].
type Command struct {
	Type     CommandType
	Text     string
	NoteID   int64
	Target   string
	Metadata map[string]string
}

func (c Command) Clone() Command {
	clone := c
	if c.Metadata != nil {
		clone.Metadata = make(map[string]string, len(c.Metadata))
		for key, value := range c.Metadata {
			clone.Metadata[key] = value
		}
	}
	return clone
}

// CommandBus delivers [Command] values to the registered handler.
type CommandBus interface {
	Dispatch(context.Context, Command) error
}

// Engine is the interface implemented by a full SpeechKit voice pipeline.
type Engine interface {
	Start(context.Context) error
	Stop(context.Context) error
	Events() <-chan Event
	Commands() CommandBus
	State() Snapshot
}

// Hooks are the lifecycle callbacks wired into a [Runtime].
// Nil hooks are silently skipped.
type Hooks struct {
	Start         func(context.Context) error
	Stop          func(context.Context) error
	HandleCommand func(context.Context, Command) error
}

// Runtime manages shared observable state and event delivery for a SpeechKit
// session. Create one with [NewRuntime] and wire it into the host application
// via [Runtime.Events] and [Runtime.Commands].
type Runtime struct {
	mu       sync.RWMutex
	snapshot Snapshot
	hooks    Hooks
	events   chan Event
	bus      commandBus
	closed   bool
}

type commandBus struct {
	runtime *Runtime
}

func NewRuntime(initial Snapshot, hooks Hooks) *Runtime {
	runtime := &Runtime{
		snapshot: initial.Clone(),
		hooks:    hooks,
		events:   make(chan Event, 64),
	}
	runtime.bus.runtime = runtime
	return runtime
}

func (r *Runtime) Start(ctx context.Context) error {
	if r.hooks.Start == nil {
		return nil
	}
	return r.hooks.Start(ctx)
}

func (r *Runtime) Stop(ctx context.Context) error {
	if r.hooks.Stop == nil {
		return nil
	}
	return r.hooks.Stop(ctx)
}

func (r *Runtime) Events() <-chan Event {
	return r.events
}

func (r *Runtime) Commands() CommandBus {
	return r.bus
}

func (r *Runtime) State() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot.Clone()
}

func (r *Runtime) SetState(snapshot Snapshot) {
	r.mu.Lock()
	r.snapshot = snapshot.Clone()
	r.mu.Unlock()
}

func (r *Runtime) UpdateState(update func(*Snapshot)) Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	if update != nil {
		update(&r.snapshot)
	}
	r.snapshot = r.snapshot.Clone()
	return r.snapshot.Clone()
}

func (r *Runtime) Publish(event Event) bool {
	if event.Type == "" {
		return false
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}

	r.mu.RLock()
	closed := r.closed
	events := r.events
	r.mu.RUnlock()
	if closed {
		return false
	}

	select {
	case events <- event:
		return true
	default:
		return false
	}
}

func (r *Runtime) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.events)
	r.mu.Unlock()
}

func (b commandBus) Dispatch(ctx context.Context, command Command) error {
	runtime := b.runtime
	if runtime == nil {
		return ErrCommandHandlerUnavailable
	}
	if runtime.hooks.HandleCommand == nil {
		return ErrCommandHandlerUnavailable
	}
	return runtime.hooks.HandleCommand(ctx, command.Clone())
}
