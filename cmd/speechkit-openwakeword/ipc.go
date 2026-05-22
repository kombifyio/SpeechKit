//go:build cgo

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type Event struct {
	Type EventType `json:"type"`
	At   time.Time `json:"at"`

	Version  string `json:"version,omitempty"`
	Backend  string `json:"backend,omitempty"`
	PhraseID string `json:"phraseId,omitempty"`

	Keyword     string  `json:"keyword,omitempty"`
	Probability float32 `json:"probability,omitempty"`
	Phrase      string  `json:"phrase,omitempty"`
	Mode        string  `json:"mode,omitempty"`
	Threshold   float32 `json:"threshold,omitempty"`

	BytesIn   int64 `json:"bytesIn,omitempty"`
	DecodesIn int64 `json:"decodesIn,omitempty"`
	UptimeSec int64 `json:"uptimeSec,omitempty"`

	Level string `json:"level,omitempty"`
	Msg   string `json:"msg,omitempty"`

	// EventDevice fields — emitted once at startup with the audio device
	// actually opened.
	DeviceID   string `json:"deviceId,omitempty"`
	DeviceName string `json:"deviceName,omitempty"`
	DeviceKind string `json:"deviceKind,omitempty"`

	// EventScore fields — emitted per decode when --debug is on.
	Score float32 `json:"score,omitempty"`

	// EventTrainingCapture fields — emitted once per WAV+JSON pair the
	// activation-capture pipeline writes to disk. v0.37.4+.
	TrainingID         string `json:"trainingId,omitempty"`
	TrainingAudioPath  string `json:"trainingAudioPath,omitempty"`
	TrainingAudioBytes int    `json:"trainingAudioBytes,omitempty"`
	TrainingPreRollMs  int    `json:"trainingPreRollMs,omitempty"`
	TrainingPostRollMs int    `json:"trainingPostRollMs,omitempty"`
}

type EventType string

const (
	EventReady           EventType = "ready"
	EventDetection       EventType = "detection"
	EventHeartbeat       EventType = "heartbeat"
	EventLog             EventType = "log"
	EventError           EventType = "error"
	EventShutdown        EventType = "shutdown"
	EventDevice          EventType = "device"
	EventScore           EventType = "score"
	EventTrainingCapture EventType = "training_capture" // v0.37.4+: one WAV/JSON pair written
)

type Command struct {
	Type CommandType `json:"type"`
}

type CommandType string

const CommandShutdown CommandType = "shutdown"

var (
	emitMu  sync.Mutex
	stdout  = os.Stdout
	encoder = json.NewEncoder(stdout)
)

func emit(ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	emitMu.Lock()
	defer emitMu.Unlock()
	if err := encoder.Encode(ev); err != nil {
		fmt.Fprintf(os.Stderr, "speechkit-openwakeword: emit failed: %v\n", err)
	}
}
