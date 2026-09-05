// The capture layer (backend registry, capture Session, device
// enumeration, frame pool) moved to the public
// pkg/speechkit/audio/capture package. This shim re-exports that surface
// so the existing device-app call sites (cmd/speechkit, internal/wakeword,
// cmd/sk-*-smoke) keep compiling unchanged. New
// capture code goes in pkg/speechkit/audio/capture; playback
// (player.go, stream_player.go) stays private in this package.
package audio

import "github.com/kombifyio/SpeechKit/pkg/speechkit/audio/capture"

// Core capture contracts.
type (
	Backend          = capture.Backend
	InputSource      = capture.InputSource
	EventType        = capture.EventType
	Event            = capture.Event
	Config           = capture.Config
	Session          = capture.Session
	Capturer         = capture.Capturer
	PooledPCMHandler = capture.PooledPCMHandler
	Factory          = capture.Factory
	DeviceInfo       = capture.DeviceInfo
	FramePool        = capture.FramePool
	Stats            = capture.Stats
)

const (
	BackendAuto                = capture.BackendAuto
	BackendWindowsWASAPIMalgo  = capture.BackendWindowsWASAPIMalgo
	BackendWindowsWASAPINative = capture.BackendWindowsWASAPINative

	InputSourceMicrophone     = capture.InputSourceMicrophone
	InputSourceSystemLoopback = capture.InputSourceSystemLoopback
	InputSourceMicAndSystem   = capture.InputSourceMicAndSystem

	EventStarted = capture.EventStarted
	EventStopped = capture.EventStopped
	EventWarning = capture.EventWarning
	EventError   = capture.EventError
	EventOverrun = capture.EventOverrun
	EventStalled = capture.EventStalled

	DefaultFrameCapacity = capture.DefaultFrameCapacity
)

// Sentinel errors re-exported for callers that branch on them.
var (
	ErrUnsupportedBackend      = capture.ErrUnsupportedBackend
	ErrBackendUnavailable      = capture.ErrBackendUnavailable
	ErrUnsupportedSource       = capture.ErrUnsupportedSource
	ErrOutputDeviceUnavailable = capture.ErrOutputDeviceUnavailable
)

// Functions and constructors (function-value aliases keep signatures in sync).
var (
	RegisterBackend       = capture.RegisterBackend
	Open                  = capture.Open
	NewCapturer           = capture.NewCapturer
	NewCapturerWithConfig = capture.NewCapturerWithConfig
	ListCaptureDevices    = capture.ListCaptureDevices
	ListOutputDevices     = capture.ListOutputDevices
	Get                   = capture.Get
	Put                   = capture.Put
)

// DefaultFramePool forwards to the capture package's pool so counters and
// recycled buffers stay shared with the capture backends. Declared as a
// pointer (Go cannot alias vars) — method calls read identically at the
// call sites.
var DefaultFramePool = &capture.DefaultFramePool

// OnCaptureDeviceRebound mirrors capture.OnCaptureDeviceRebound for
// existing assignment sites (cmd/speechkit assigns audio.OnCaptureDeviceRebound).
// Go cannot alias vars, so the capture package's hook is wired once at
// init to forward to whatever this var currently holds.
var OnCaptureDeviceRebound func(oldID, newID, name string)

func init() {
	capture.OnCaptureDeviceRebound = func(oldID, newID, name string) {
		if OnCaptureDeviceRebound != nil {
			OnCaptureDeviceRebound(oldID, newID, name)
		}
	}
}
