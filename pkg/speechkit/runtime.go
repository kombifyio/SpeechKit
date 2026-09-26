// Package-level documentation lives in doc.go.

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

// Event types published by the framework and its hosts. The string values are
// stable identifiers that consumers switch on; hosts may publish further types
// of their own. The comments name the [Event] fields each type fills.
const (
	// EventStateChanged reports a pipeline status change with no more specific
	// type; Message carries the status and Text its detail.
	EventStateChanged EventType = "state.changed"
	// EventRecordingStarted means microphone capture began.
	EventRecordingStarted EventType = "recording.started"
	// EventProcessingStarted means capture ended and transcription or Assist
	// processing is in flight.
	EventProcessingStarted EventType = "processing.started"
	// EventTranscriptionDraft carries live provider draft text in Text; drafts
	// never reach output or persistence.
	EventTranscriptionDraft EventType = "transcription.draft"
	// EventTranscriptionReady means a transcription finished ("done" status).
	EventTranscriptionReady EventType = "transcription.ready"
	// EventTranscriptCommitted carries a final transcript in Text and Provider;
	// QuickNote marks quick-note captures.
	EventTranscriptCommitted EventType = "transcription.committed"
	// EventQuickNoteModeArmed means the next recording goes into a quick note
	// instead of the focused application.
	EventQuickNoteModeArmed EventType = "quicknote.mode_armed"
	// EventQuickNoteUpdated means committed text was written to a quick note.
	EventQuickNoteUpdated EventType = "quicknote.updated"
	// EventWarningRaised and EventErrorRaised mirror "warn" and "error" log
	// lines; Message carries the text and Err the error when one is known.
	EventWarningRaised EventType = "warning.raised"
	EventErrorRaised   EventType = "error.raised"
	// EventShortcutMatched means a transcript matched a codeword, shortcut or
	// customization action; Shortcut names the matched action kind.
	EventShortcutMatched EventType = "shortcut.matched"
	// EventWakeFired means a wake word was detected; Message carries the
	// phrase and Mode the mode it activates.
	EventWakeFired EventType = "wake.fired"
	// EventSkillExecuted means a quick action or Assist skill completed.
	EventSkillExecuted EventType = "skill.executed"
	// EventCompanionSessionStarted and EventCompanionSessionEnded bracket a
	// hands-free Companion session.
	EventCompanionSessionStarted EventType = "companion.session.started"
	EventCompanionSessionEnded   EventType = "companion.session.ended"
	// EventVoiceAgentTurnFinalized carries one finalized dialogue turn: Text
	// is the turn and Message the speaker role.
	EventVoiceAgentTurnFinalized EventType = "voiceagent.turn.finalized"
	// EventTTSStarted and EventTTSFinished bracket spoken playback of an
	// Assist result; Text carries the spoken text.
	EventTTSStarted  EventType = "tts.started"
	EventTTSFinished EventType = "tts.finished"
	// EventCustomizationAction carries, in CustomizationActions, an action
	// from Words and Replacements that the host did not execute itself.
	EventCustomizationAction EventType = "customization.action"
)

// Event is a notification published to the event channel returned by
// [Runtime.Events]. Consumers should switch on Type and inspect the
// relevant fields.
type Event struct {
	Type                 EventType
	Time                 time.Time
	Message              string
	Text                 string
	Provider             string
	Mode                 string
	SessionID            string
	QuickNote            bool
	Err                  error
	Shortcut             string
	Metadata             *Metadata
	CustomizationActions []CustomizationAction
}

// Clone returns a deep copy of the event: Metadata and CustomizationActions
// (including their Payload maps) are copied, so publisher and consumers never
// share mutable state.
func (e Event) Clone() Event {
	clone := e
	clone.Metadata = e.Metadata.Clone()
	clone.CustomizationActions = cloneCustomizationActions(e.CustomizationActions)
	return clone
}

func cloneCustomizationActions(actions []CustomizationAction) []CustomizationAction {
	if len(actions) == 0 {
		return nil
	}
	out := make([]CustomizationAction, 0, len(actions))
	for _, action := range actions {
		clone := action
		if len(action.Payload) > 0 {
			clone.Payload = make(map[string]any, len(action.Payload))
			for key, value := range action.Payload {
				clone.Payload[key] = value
			}
		}
		out = append(out, clone)
	}
	return out
}

// Metadata carries optional event key/value data without making Event
// non-comparable for existing SDK consumers.
type Metadata map[string]string

// NewMetadata copies values into a fresh [Metadata]. It returns nil for an
// empty map so events without metadata stay cheap and comparable.
func NewMetadata(values map[string]string) *Metadata {
	if len(values) == 0 {
		return nil
	}
	clone := make(Metadata, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return &clone
}

// Clone returns a copy of the metadata, or nil when the receiver is nil or
// empty.
func (m *Metadata) Clone() *Metadata {
	if m == nil || len(*m) == 0 {
		return nil
	}
	clone := make(Metadata, len(*m))
	for key, value := range *m {
		clone[key] = value
	}
	return &clone
}

// Get returns the value stored under key, or "" when the key is absent or the
// receiver is nil.
func (m *Metadata) Get(key string) string {
	if m == nil {
		return ""
	}
	return (*m)[key]
}

// Map returns the metadata as a plain map copy, or nil when the receiver is
// nil or empty. Mutating the result does not affect the event.
func (m *Metadata) Map() map[string]string {
	if m == nil || len(*m) == 0 {
		return nil
	}
	clone := make(map[string]string, len(*m))
	for key, value := range *m {
		clone[key] = value
	}
	return clone
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

// Clone returns a copy of the snapshot with its own Providers slice and
// ActiveProfiles map, so holders never race the [Runtime].
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

// Command types the desktop host's handler understands; hosts may define
// further types, and the handler rejects unknown ones.
const (
	// CommandShowDashboard brings the dashboard window to the front;
	// Metadata["source"] records who asked.
	CommandShowDashboard CommandType = "dashboard.show"
	// CommandStartDictation and CommandStopDictation start and stop a
	// microphone capture for the active mode, as the hotkey does; an optional
	// Metadata["label"] is logged with the capture.
	CommandStartDictation CommandType = "dictation.start"
	CommandStopDictation  CommandType = "dictation.stop"
	// CommandStartMode and CommandStopMode start and stop the mode named by
	// Metadata["mode"]; the hands-free layer and the /api/v1 control plane
	// use them.
	CommandStartMode CommandType = "mode.start"
	CommandStopMode  CommandType = "mode.stop"
	// CommandSetActiveMode switches the active mode to Metadata["mode"]
	// without starting a capture.
	CommandSetActiveMode CommandType = "mode.set_active"
	// CommandOpenQuickNote opens the editor of the quick note NoteID.
	CommandOpenQuickNote CommandType = "quicknote.open"
	// CommandOpenQuickCapture and CommandCloseQuickCapture show and hide the
	// quick capture window.
	CommandOpenQuickCapture  CommandType = "quicknote.capture.open"
	CommandCloseQuickCapture CommandType = "quicknote.capture.close"
	// CommandArmQuickNoteRecording routes the next recording into the quick
	// note NoteID.
	CommandArmQuickNoteRecording CommandType = "quicknote.record.arm"
	// CommandCopyLastTranscription and CommandInsertLastTranscription copy the
	// most recent transcription to the clipboard or insert it at Target.
	CommandCopyLastTranscription   CommandType = "transcription.copy_last"
	CommandInsertLastTranscription CommandType = "transcription.insert_last"
	// CommandSummarizeSelection summarizes the selected text through Assist;
	// Text carries an optional instruction.
	CommandSummarizeSelection CommandType = "selection.summarize"
)

// Command is a request dispatched through the [CommandBus].
type Command struct {
	Type     CommandType
	Text     string
	NoteID   int64
	Target   string
	Metadata map[string]string
}

// Clone returns a copy of the command with its own Metadata map.
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

// NewRuntime creates a Runtime whose observable state starts as a copy of
// initial and whose [Hooks] run on Start, Stop and command dispatch. The event
// channel is buffered with 64 slots; see [Runtime.Publish].
func NewRuntime(initial Snapshot, hooks Hooks) *Runtime {
	runtime := &Runtime{
		snapshot: initial.Clone(),
		hooks:    hooks,
		events:   make(chan Event, 64),
	}
	runtime.bus.runtime = runtime
	return runtime
}

// Start runs the Start hook and returns its error, or nil when no hook is
// configured.
func (r *Runtime) Start(ctx context.Context) error {
	if r.hooks.Start == nil {
		return nil
	}
	return r.hooks.Start(ctx)
}

// Stop runs the Stop hook and returns its error, or nil when no hook is
// configured. It leaves the event channel open; see [Runtime.Close].
func (r *Runtime) Stop(ctx context.Context) error {
	if r.hooks.Stop == nil {
		return nil
	}
	return r.hooks.Stop(ctx)
}

// Events returns the runtime's event channel. The channel is buffered with
// 64 slots; [Runtime.Publish] never blocks and drops events once the buffer
// is full, so consumers must drain the channel promptly to avoid losing
// events.
func (r *Runtime) Events() <-chan Event {
	return r.events
}

// Commands returns the bus that routes [Command] values to the HandleCommand
// hook. Dispatch clones each command and returns
// [ErrCommandHandlerUnavailable] when no hook is configured.
func (r *Runtime) Commands() CommandBus {
	return r.bus
}

// State returns a copy of the current snapshot; safe from any goroutine.
func (r *Runtime) State() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot.Clone()
}

// SetState replaces the snapshot with a copy of snapshot. It publishes no
// event.
func (r *Runtime) SetState(snapshot Snapshot) {
	r.mu.Lock()
	r.snapshot = snapshot.Clone()
	r.mu.Unlock()
}

// UpdateState applies update to the snapshot under the runtime lock and
// returns a copy of the result. A nil update only reads the current state.
func (r *Runtime) UpdateState(update func(*Snapshot)) Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	if update != nil {
		update(&r.snapshot)
	}
	r.snapshot = r.snapshot.Clone()
	return r.snapshot.Clone()
}

// Publish delivers event to the channel returned by [Runtime.Events]. It
// never blocks: the event channel is buffered with 64 slots, and when the
// buffer is full (or the runtime is closed, or event.Type is empty) the
// event is silently dropped. The bool return reports whether the event was
// actually delivered to the buffer. Slow consumers must drain the events
// channel promptly, or events will be lost.
func (r *Runtime) Publish(event Event) bool {
	if event.Type == "" {
		return false
	}
	event = event.Clone()
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

// Close closes the event channel so consumers ranging over [Runtime.Events]
// finish; later [Runtime.Publish] calls report false. Close is idempotent.
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
