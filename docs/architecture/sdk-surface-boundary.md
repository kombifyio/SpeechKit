# SDK Surface Boundary

Decision date: 2026-05-26. This document records the v0.40 SDK-surface
boundary so embedders can consume SpeechKit without importing desktop or
server internals.

Last updated: 2026-06-02 for the provider-neutral Speaker Layer surface.

## Purpose

`pkg/speechkit` is the reusable framework boundary. It must compile for
Local-Library hosts without depending on `internal/*`, Wails, Windows-only
adapters, desktop storage, server middleware, or bundled app assets.

The Android twin — which Gradle modules a host may depend on — is
[android-sdk-surface-boundary.md](android-sdk-surface-boundary.md).

The v0.40 line promoted the embeddable Voice-Companion building blocks that
were previously only available through product adapters. This hardening branch
is an explicit breaking cleanup: host applications should import public SDK
packages and leave Go `internal` packages to this repository's own binaries.

## Public Packages

| Package | Public responsibility |
|---------|-----------------------|
| `pkg/speechkit` | Runtime, mode contracts, event bus, provider catalog, readiness, and top-level service contracts. |
| `pkg/speechkit/assist` | Embeddable Assist service with generator, tools, multi-turn session context, codeword routing, and optional TTS routing. |
| `pkg/speechkit/assist/genkitadapter` | Optional adapter from Genkit-style generators to the public Assist generator contract. |
| `pkg/speechkit/wakeword` | Wake-word phrase catalog, detection events, dispatcher, detector contracts, and AutoEndPolicy. |
| `pkg/speechkit/wakeword/sherpa` | Sherpa-onnx adapter behind the public wake-word detector contracts, with cgo/no-cgo build behavior. |
| `pkg/speechkit/tts` | Provider, ProviderKind, Router, Service, fallback strategy, synthesis options, and result contract. |
| `pkg/speechkit/companion` | `NewHandsFree(...)` composer for hands-free target routing across Assist, Voice Agent, and UI-assisted Dictation using wake detections, host transcript requests, optional TTS, and EventBus lifecycle. |
| `pkg/speechkit/speaker` | Provider-neutral speaker options, diarization results, speaker words/segments, provider profiles, streaming audio format, and `SpeakerFrame` contracts. |

## Boundary Rules

1. Public SDK packages must not import `internal/*`.
2. Public SDK contracts should use small interfaces and plain Go values so
   desktop/server callers can adapt concrete providers without pulling app
   dependencies into embedders.
3. Target-specific concerns stay outside `pkg/speechkit`: Wails, hotkeys,
   tray, Windows Credential Manager, server auth middleware, HTTP route
   plumbing, and concrete bundled provider initialization.
4. Optional provider integrations belong behind adapters. The SDK owns the
   contract; desktop/server code owns concrete runtime factories.
5. Additive event types are allowed in patch releases. Removing event types,
   changing struct field semantics, or changing exported method signatures
   requires a release-plan callout and an API diff.
6. Deprecated exported fields and methods may be removed only on a branch that
   documents the break and carries the `breaking-api-approved` PR label. This
   branch removes the `LiveConfig.Instruction` and `LiveConfig.SystemPrompt`
   aliases; embedders must use `LiveConfig.FrameworkPrompt`.

## Current Verification

Verified on 2026-05-26 and updated on 2026-05-27 and 2026-06-02:

- `go test ./pkg/speechkit/...` passes with `CGO_ENABLED=0`.
- `go test ./pkg/speechkit/...` passes with MinGW cgo enabled.
- `go test ./examples/embed-companion ./examples/embed-tts ./examples/embed-event-bus` passes.
- Public export dry-run includes `pkg/speechkit/wakeword`, `pkg/speechkit/companion`, and `pkg/speechkit/tts`.
- Production SDK packages have no `internal/*` imports.
- Shared parity tests in `internal/sdkparity` exercise the same public/internal
  TTS Router provider-kind behavior and wakeword Dispatcher/AutoEnd behavior;
  the parity harness is test-only and does not change the production SDK
  import boundary.
- CI public API stability discovers the surface dynamically with
  `go list ./pkg/speechkit/...`, so new promoted SDK packages are checked by
  default.
- The public consumer-smoke gate validates a fresh external Go module that
  imports `github.com/kombifyio/SpeechKit/pkg/speechkit/{assist,companion,tts,wakeword}`
  from a clean public export, wires `companion.TargetAssist`, and builds
  without depending on public-invisible `internal/*` packages.
- 2026-06-02 update: `go test ./pkg/speechkit/speaker` passes and the package
  carries the provider-neutral Speaker Layer contracts. Provider adapters remain
  internal so embedders can depend on the contract without importing runtime
  provider code.

## Release Gates

- API diff for every package returned by `go list ./pkg/speechkit/...` should
  show no breaking removals unless the PR has the `breaking-api-approved` label.
- Docker/server release gates must pass before a public tag is cut.
- Public source exports must include the SDK, self-host server, CLI, MCP, docs,
  and examples needed by agents, while excluding desktop source, installer
  source, releaseguard tooling, and E2E fixtures.
- The external consumer smoke is a required public export gate before claiming
  the SDK surface is embeddable.
