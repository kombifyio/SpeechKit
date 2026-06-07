package lifecycle

import (
	"context"
	"time"
)

// ModeKey identifies one of the three SpeechKit strict modes.
//
// Hosts may extend the value space (e.g. for experimental modes) but
// the three canonical keys below are guaranteed to be recognised by
// the Device-Target and Server-Target adapter layers.
type ModeKey string

const (
	ModeDictation  ModeKey = "dictation"
	ModeAssist     ModeKey = "assist"
	ModeVoiceAgent ModeKey = "voiceagent"
)

// SharedDepKey identifies a refcounted shared dependency in
// [SharedDepRegistry]. Values are opaque strings — the lifecycle
// package does not interpret them. Hosts choose stable names that
// match their own module organisation (e.g. "audio.capture",
// "stt.router", "ai.genkit"). Two ModeRuntime implementations that
// declare the same key MUST be able to consume the same underlying
// value type.
type SharedDepKey string

// Deps is the dependency bundle delivered to [ModeRuntime.Start] by
// the registry. The Values map contains exactly the keys this runtime
// declared via [ModeRuntime.Requires], with values produced by the
// factories registered in [SharedDepRegistry].
//
// Helpers below provide typed access; ModeRuntime implementations
// should never reach into Values directly without a type assertion.
type Deps struct {
	Values map[SharedDepKey]any
}

// Get returns the value associated with key and whether the key was
// present. Callers that depend on a key MUST have declared it via
// Requires() — a missing key is a programmer error in the host wiring.
func (d Deps) Get(key SharedDepKey) (any, bool) {
	if d.Values == nil {
		return nil, false
	}
	v, ok := d.Values[key]
	return v, ok
}

// MustGet returns the value or panics. Use sparingly — only in test
// fakes and in ModeRuntime.Start where the missing-key path is a
// non-recoverable wiring bug.
func (d Deps) MustGet(key SharedDepKey) any {
	v, ok := d.Get(key)
	if !ok {
		panic("lifecycle: required dependency missing: " + string(key))
	}
	return v
}

// ModeRuntime is the contract every SpeechKit mode implements so the
// Registry can orchestrate its lifecycle.
//
// Implementations MUST:
//
//   - Be idempotent: Start on a Running runtime returns nil; Stop on a
//     Stopped runtime returns nil.
//   - Terminate every goroutine they spawned before Stop returns.
//   - Treat the deps map as borrowed: don't retain references past Stop.
//   - Return the same Requires() list across the lifetime of the
//     instance. The registry caches this set; mid-life changes are
//     undefined behaviour.
//
// The contract is verified by [TestRegistryLeak] in registry_leak_test.go.
type ModeRuntime interface {
	// Name returns the mode identity. The registry uses Name() to key
	// runtimes; duplicate registrations are rejected at Register time.
	Name() ModeKey

	// Start brings the runtime to Running. The deps map carries values
	// for every key returned by Requires(). Returns error if any
	// startup step fails; the registry treats this as a Failed
	// transition and releases any shared deps it acquired on the
	// runtime's behalf.
	Start(ctx context.Context, deps Deps) error

	// Stop tears the runtime down to Stopped. Idempotent. MUST drain
	// goroutines, close channels, and free any state derived from
	// borrowed deps before returning.
	Stop(ctx context.Context) error

	// Status returns the current lifecycle state. The registry treats
	// this as authoritative for diff computations and observability —
	// implementations must keep it consistent with actual goroutine /
	// resource ownership.
	Status() Status

	// Requires returns the SharedDepKeys this runtime needs as Deps
	// inputs. The slice is consumed at every Start; implementations
	// should return a stable copy or a pre-built constant.
	Requires() []SharedDepKey
}

// WarmableModeRuntime is an optional extension for runtimes that can prepare
// heavyweight dependencies before Start flips the mode to Running. Registry
// callers opt in through ApplyWithOptions; plain Apply never calls Warmup.
//
// Warmup receives the same already-acquired shared dependencies as Start. It
// must not retain borrowed dependency values past Stop.
type WarmableModeRuntime interface {
	ModeRuntime
	Warmup(ctx context.Context, deps Deps) error
}

// TransitionEvent is published by [Registry.Subscribe] consumers.
// One event is emitted per status change.
//
// Hosts wire the audit-log on top of this stream — see
// docs/compliance/audit-event-catalog.md "Mode lifecycle" for the
// canonical event-name mapping.
type TransitionEvent struct {
	Mode      ModeKey
	From      Status
	To        Status
	Err       error // populated when To == StatusFailed
	Timestamp time.Time
}
