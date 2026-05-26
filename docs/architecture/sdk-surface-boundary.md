# SDK Surface Boundary

Decision date: 2026-05-26. This document records the v0.40.1 SDK-surface
boundary so embedders can consume SpeechKit without importing desktop or
server internals.

## Purpose

`pkg/speechkit` is the reusable framework boundary. It must compile for
Local-Library hosts without depending on `internal/*`, Wails, Windows-only
adapters, desktop storage, server middleware, or bundled app assets.

The v0.40.1 implementation branch promotes the embeddable Voice-Companion
building blocks that were previously reachable only through internal code.
The release is additive and must branch after the v0.40.0 tag so the API
diff can prove there are no signature breaks against v0.40.0.

## Public Packages

| Package | Public responsibility |
|---------|-----------------------|
| `pkg/speechkit` | Runtime, mode contracts, event bus, provider catalog, readiness, and top-level service contracts. |
| `pkg/speechkit/assist` | Embeddable Assist service with generator, tools, multi-turn session context, codeword routing, and optional TTS routing. |
| `pkg/speechkit/assist/genkitadapter` | Optional adapter from Genkit-style generators to the public Assist generator contract. |
| `pkg/speechkit/wakeword` | Wake-word phrase catalog, detection events, dispatcher, detector contracts, and AutoEndPolicy. |
| `pkg/speechkit/wakeword/sherpa` | Sherpa-onnx adapter behind the public wake-word detector contracts, with cgo/no-cgo build behavior. |
| `pkg/speechkit/tts` | Provider, ProviderKind, Router, Service, fallback strategy, synthesis options, and result contract. |
| `pkg/speechkit/companion` | `NewHandsFree(...)` composer for wake detections, host transcript requests, Assist, optional TTS, and EventBus lifecycle. |

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

## Current Verification

Verified on 2026-05-26:

- `go test ./pkg/speechkit/...` passes with `CGO_ENABLED=0`.
- `go test ./pkg/speechkit/...` passes with MinGW cgo enabled.
- `go test ./internal/wakeword/... ./internal/tts/... ./internal/assist/...` passes.
- `go test ./examples/embed-companion ./examples/embed-tts ./examples/embed-event-bus` passes.
- Public export dry-run includes `pkg/speechkit/wakeword`, `pkg/speechkit/companion`, and `pkg/speechkit/tts`.
- Production SDK packages have no `internal/*` imports.

## Release Gates

- v0.40.0 must be tagged before v0.40.1 branches.
- API diff for `pkg/speechkit/...` must compare v0.40.1 against the v0.40.0 tag and show additions only.
- Docker/server release gate still needs to run before the patch is cut.
- Internal alias/adaptor cleanup remains tracked until public and internal wake/TTS behavior share ownership or parity tests.
