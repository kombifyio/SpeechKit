// Package lifecycle owns mode start/stop orchestration and refcounted shared
// dependencies for SpeechKit hosts.
//
// SpeechKit exposes three strict modes (Dictation, Assist, Voice Agent). A
// host that toggles a mode off at runtime should release everything that
// mode kept alive — Genkit runtime, TTS router, STT router, audio capture,
// the Voice Agent session manager, etc. Without a coordination layer, hosts
// either (a) leak goroutines and provider state forever, or (b) bake mode
// gating into ad-hoc if/else bootstrap code that is impossible to keep
// symmetric between Device-Target and Server-Target.
//
// This package provides that coordination layer as a framework-neutral,
// dependency-free public API. It knows nothing about audio, STT, TTS, or
// LLM concretely — those live in their own internal packages and are
// surfaced here through SharedDepKey identifiers + factory callbacks
// registered by the host.
//
// # Two-layer model
//
//   - [Registry] orchestrates mode runtimes (which modes are on / off).
//     Hosts call [Registry.Apply] with a target set; the registry diffs
//     current state, stops modes that should be off, starts modes that
//     should be on, and emits [TransitionEvent] values to subscribers.
//
//   - [SharedDepRegistry] holds refcounted shared dependencies. A mode
//     runtime declares its dependency keys via [ModeRuntime.Requires];
//     the registry acquires them on Start and releases them on Stop.
//     A dep is lazily constructed on the first Acquire and torn down
//     when its last holder releases.
//
// # Mode runtime contract
//
// A [ModeRuntime] implementation is responsible for translating an
// [Deps] map (delivered by the registry on Start) into whatever live
// state the mode needs (handlers, hot loops, background workers).
// Implementations MUST be idempotent on Start and Stop and MUST tear
// down all goroutines they spawned before returning from Stop —
// see [TestRegistryLeak] for the enforcement contract.
//
// Runtimes that can cheaply validate or preload heavyweight resources before
// Start may also implement [WarmableModeRuntime]. Hosts opt in with
// [Registry.ApplyWithOptions] and [ApplyOptions.EagerWarmup]; plain
// [Registry.Apply] preserves the existing cold-start behavior.
//
// # Audit events
//
// Hosts wire audit logging on top of [Registry.Subscribe] — the
// lifecycle package does not depend on the audit package. The
// canonical event names are documented in
// docs/compliance/audit-event-catalog.md under "Mode lifecycle":
//
//   - speechkit.mode.start (Outcome=success | failure)
//   - speechkit.mode.stop  (Outcome=success | failure)
//   - speechkit.mode.failed (transition aborted by a dependency)
//
// # Stability
//
// pkg/speechkit/lifecycle is part of the OSS public surface and follows
// the same semver discipline as the rest of pkg/speechkit. Before v1.0
// the API may still evolve — see [CHANGELOG.md] for breaking-change calls.
//
// [CHANGELOG.md]: https://github.com/kombifyio/SpeechKit/blob/main/CHANGELOG.md
package lifecycle
