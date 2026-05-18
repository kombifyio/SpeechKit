//go:build cgo

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Event is one stdout line in the sidecar protocol. Fields are typed so
// host code (cmd/speechkit/desktop_wakeword.go) can parse without ad-hoc
// JSON walking, and so multiple client adapters (Local-Target CLI, future
// gomobile shells) can re-use the same struct.
type Event struct {
	Type EventType `json:"type"`
	At   time.Time `json:"at"`

	// EventReady fields — emitted once at startup.
	Version  string `json:"version,omitempty"`
	Backend  string `json:"backend,omitempty"`
	PhraseID string `json:"phraseId,omitempty"`

	// EventDetection fields.
	Keyword     string  `json:"keyword,omitempty"`
	Probability float32 `json:"probability,omitempty"`

	// Shared between Ready + Detection: display phrase + active mode.
	Phrase    string  `json:"phrase,omitempty"`
	Mode      string  `json:"mode,omitempty"`
	Threshold float32 `json:"threshold,omitempty"`

	// EventHeartbeat fields.
	BytesIn   int64 `json:"bytesIn,omitempty"`
	DecodesIn int64 `json:"decodesIn,omitempty"`
	UptimeSec int64 `json:"uptimeSec,omitempty"`

	// EventLog / EventError fields.
	Level string `json:"level,omitempty"`
	Msg   string `json:"msg,omitempty"`
}

// EventType enumerates the discriminator values used on stdout. New types
// are additive — older host adapters MUST tolerate unknown types by
// ignoring the event (they should never crash on a forward-compatible
// addition).
type EventType string

const (
	EventReady     EventType = "ready"
	EventDetection EventType = "detection"
	EventHeartbeat EventType = "heartbeat"
	EventLog       EventType = "log"
	EventError     EventType = "error"
	EventShutdown  EventType = "shutdown"
)

// Command is one stdin line in the sidecar protocol. The first version
// only supports an explicit shutdown — restart-on-config-change is handled
// by the host respawning the process with new flags.
type Command struct {
	Type CommandType `json:"type"`
}

// CommandType enumerates accepted stdin command verbs.
type CommandType string

const (
	CommandShutdown CommandType = "shutdown"
)

var (
	emitMu  sync.Mutex
	stdout  = os.Stdout
	encoder = json.NewEncoder(stdout)
)

// emit serialises an Event to stdout as a single line. Serialisation is
// guarded by a mutex so concurrent emitters from heartbeat / detection /
// log goroutines never interleave half-written JSON. Encoder.Encode
// already terminates the line with '\n', no extra newline needed.
func emit(ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	emitMu.Lock()
	defer emitMu.Unlock()
	if err := encoder.Encode(ev); err != nil {
		// Last-resort: write a plain-text error to stderr; the host's
		// stderr-fanout will pick it up. We do not return — the sidecar
		// should keep running even if one event failed to serialise.
		fmt.Fprintf(os.Stderr, "speechkit-wakeword: emit failed: %v\n", err)
	}
}
