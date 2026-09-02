# SDK Surface Boundary

Decision date: 2026-05-26. This document records the v0.40 SDK-surface
boundary so embedders can consume SpeechKit without importing desktop or
server internals.

Last updated: 2026-08-28 with the full `go list ./pkg/speechkit/...` package
inventory.

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

The surface is discovered dynamically with `go list ./pkg/speechkit/...`; this
table mirrors that inventory.

| Package | Public responsibility |
|---------|-----------------------|
| `pkg/speechkit` | Contracts and value types only: modes, capabilities, provider profiles, runtime policy, pipeline interfaces (`AudioRecorder`, `Transcriber`, `SegmentCollector`, `TranscriptOutput`, `JobSubmitter`), the values that flow between them (`Submission`, `Transcript`, `TranscriptionJob`), `Runtime` and the event bus. Imports no sibling package, so every subpackage can depend on it. |
| `pkg/speechkit/agentbridge` | Framework-neutral seam for driving an external coding agent (prompt in, normalized events out). |
| `pkg/speechkit/agentbridge/codex` | Drives the official OpenAI Codex binary as an agentbridge agent. |
| `pkg/speechkit/agentbridge/voicetools` | Binds an `agentbridge.Agent` to the Voice Agent's tool surface. |
| `pkg/speechkit/agentkit` | Go harness for building Voice Agent hosts: tool registry, session memory, lifecycle hooks. |
| `pkg/speechkit/assist` | Embeddable Assist service with generator, tools, multi-turn session context, codeword routing, and optional TTS routing. |
| `pkg/speechkit/assist/genkitadapter` | Optional adapter from Genkit-style generators to the public Assist generator contract. |
| `pkg/speechkit/assist/skills` | Voice-Companion skill catalog (Time, and friends) for Assist hosts. |
| `pkg/speechkit/assist/toolbridge` | Adapts Assist-mode tools to other tool-calling surfaces. |
| `pkg/speechkit/audio` | Shared PCM audio primitives: 16kHz S16 mono constants, WAV framing, duration and level math. |
| `pkg/speechkit/audio/capture` | Microphone and system-loopback capture. `Session` satisfies `speechkit.AudioRecorder`, so a host does not implement recording itself; `RegisterBackend` is the extension point, and builds without a native backend report `ErrBackendUnavailable`. |
| `pkg/speechkit/catalog` | Built-in provider/model catalog: `DefaultCatalog`, `Catalog.With`, `DefaultProviderProfiles`, the provider defaults matrix and the model registry with freshness metadata. Data only; contracts stay in the root package. |
| `pkg/speechkit/client` | Typed HTTP client for talking to a remote SpeechKit Server. |
| `pkg/speechkit/companion` | `NewHandsFree(...)` composer for hands-free target routing across Assist, Voice Agent, and UI-assisted Dictation using wake detections, host transcript requests, optional TTS, and EventBus lifecycle. |
| `pkg/speechkit/customize` | Public Words/Replacements customization contract. |
| `pkg/speechkit/deviceagent` | Credential-minimal LAN-side SpeechKit device agent. |
| `pkg/speechkit/dictation` | Embeddable strict Dictation runtime. |
| `pkg/speechkit/hostconfig` | Turns a SpeechKit TOML configuration file into the public SDK configuration types. |
| `pkg/speechkit/internal/speakercontract` | Internal test-only speaker conformance helpers; not importable by embedders. |
| `pkg/speechkit/lifecycle` | Mode start/stop orchestration and refcounted shared resources. |
| `pkg/speechkit/localization` | Resolves stable SpeechKit message IDs against BCP-47 locales. |
| `pkg/speechkit/netsec` | Centralized network security primitives shared by public surfaces. |
| `pkg/speechkit/pipeline` | Composable capture-to-transcript engine: `RecordingController`, `TranscriptionWorker`, `TranscriptionRunner`, `DictationSegmenter`, `TranscriptSessionLedger`, live-commit policy. Implements the root-package contracts; `dictation` composes it. |
| `pkg/speechkit/procguard` | Ties long-lived child processes to the lifetime of the host process. |
| `pkg/speechkit/provideropts` | Provider-neutral voice option manifest (per-provider native options). |
| `pkg/speechkit/speaker` | Provider-neutral speaker options, diarization results, speaker words/segments, provider profiles, streaming audio format, and `SpeakerFrame` contracts. |
| `pkg/speechkit/storage` | Storage-backend contract: capabilities, install/device/user/tenant scopes, and backend configuration. |
| `pkg/speechkit/stt` | Speech-to-text contracts: provider interface, transcribe options, result, router, the `AsTranscriber` bridge, and the helpers the adapters share. It names no provider, so importing it costs 49 external packages. |
| `pkg/speechkit/stt/allproviders` | Batteries assembly: every shipped provider plus `BuildRouter`, `EnabledProviders`, and the provider registry. Import it when the host offers a provider choice at runtime. |
| `pkg/speechkit/stt/assemblyai` | AssemblyAI: sync transcription, speaker streaming with attribution, live dictation with optional LLM turn cleanup. |
| `pkg/speechkit/stt/deepgram` | Deepgram: batch transcription, speaker streaming, live dictation, and the Flux turn stream. |
| `pkg/speechkit/stt/google` | Google Cloud Speech-to-Text. Importing it pulls the Google Cloud client and gRPC stack. |
| `pkg/speechkit/stt/huggingface` | HuggingFace Inference API transcription. |
| `pkg/speechkit/stt/local` | Built-in whisper.cpp subprocess provider plus its model and runtime helpers. |
| `pkg/speechkit/stt/openaicompat` | One adapter for every OpenAI-compatible audio endpoint: OpenAI, Groq, Ollama. |
| `pkg/speechkit/stt/openrouter` | OpenRouter transcription. |
| `pkg/speechkit/stt/sttcontract` | Reusable conformance suite for STT provider implementations. |
| `pkg/speechkit/stt/vps` | Self-hosted whisper-server: an OpenAI-compatible endpoint the user runs themselves. |
| `pkg/speechkit/tts` | Provider, ProviderKind, Router, Service, fallback strategy, synthesis options, and result contract. |
| `pkg/speechkit/tts/ttscontract` | Reusable conformance suite for TTS provider implementations. |
| `pkg/speechkit/ttsroute` | Single source of truth mapping a Voice-Output selection to a TTS route. |
| `pkg/speechkit/voiceagent` | Embeddable Voice Agent service (realtime audio-to-audio mode). |
| `pkg/speechkit/voiceagent/cascaded` | Turn-based STT -> LLM -> TTS voice-agent pipeline fallback. |
| `pkg/speechkit/voiceagent/live` | Voice Agent realtime-protocol types, session runtime, and the `LiveProvider` contract. It names no provider, so importing it costs 15 external packages. |
| `pkg/speechkit/voiceagent/live/assemblyai` | AssemblyAI Voice Agent realtime provider. |
| `pkg/speechkit/voiceagent/live/deepgram` | Deepgram Voice Agent realtime provider. |
| `pkg/speechkit/voiceagent/live/foundry` | Microsoft Foundry adapter over the OpenAI Realtime provider. |
| `pkg/speechkit/voiceagent/live/gemini` | Gemini Live realtime provider. |
| `pkg/speechkit/voiceagent/live/allproviders` | Batteries assembly for the realtime providers: resolves a provider id, alias or profile id to a live provider. Import it when the host offers a provider choice at runtime. |
| `pkg/speechkit/voiceagent/live/openai` | OpenAI Realtime provider, including its client-side response cancel. |
| `pkg/speechkit/voiceagent/live/livecontract` | Reusable conformance checks for `LiveProvider` implementations. |
| `pkg/speechkit/voiceagent/local` | `voiceagent.Provider` on top of an in-process local pipeline. |
| `pkg/speechkit/wakeword` | Wake-word phrase catalog, detection events, dispatcher, detector contracts, and AutoEndPolicy. |
| `pkg/speechkit/wakeword/sherpa` | Sherpa-onnx adapter behind the public wake-word detector contracts, with cgo/no-cgo build behavior. |

## Native Requirements

Most of `pkg/speechkit` is pure Go and cross-compiles with `CGO_ENABLED=0`.
The exceptions are listed here so an embedder can see up front what a
package needs beyond `go get`. Every package in the table compiles on every
platform; the native dependency is only needed at runtime, and a missing one
is reported at construction time with a sentinel error, never as a panic or
a silent no-op.

| Package | Build | Runtime dependency | Behaviour when missing |
| --- | --- | --- | --- |
| `pkg/speechkit/audio/capture` | `windows && cgo` for the WASAPI backend (malgo/miniaudio, needs a C toolchain such as MinGW). Pure Go elsewhere. | None beyond the toolchain. | `Open`/`NewCapturer` return an error wrapping `ErrBackendUnavailable`; `RegisterBackend` lets a host plug in its own capture on other platforms. |
| `pkg/speechkit/wakeword` | `cgo` for `NewDetector`/`NewPipeline` (sherpa-onnx via `sherpa-onnx-go`). Contracts, phrase catalog, dispatcher and `AutoEndPolicy` are pure Go. | sherpa-onnx shared libraries and a KWS model on disk. | `NewDetector` and `NewPipeline` return `ErrCgoRequired` in a `CGO_ENABLED=0` build; the sentinel exists in every build so `errors.Is` checks compile everywhere. |
| `pkg/speechkit/wakeword/sherpa` | Same as `wakeword`. | Same as `wakeword`. | Compiles against the no-cgo surface; the adapter reports `wakeword.ErrCgoRequired`. |
| `pkg/speechkit/stt/local` | Pure Go. | A `whisper-server` executable next to the host binary or in the managed install directory (`%LOCALAPPDATA%\SpeechKit\bin` on Windows); `SPEECHKIT_ALLOW_WHISPER_PATH=1` also allows `PATH` lookup. A GGML model file. | `StartServer`/`FindWhisperBinary` return a descriptive error; `VerifyInstallation` reports what is missing without starting anything. |
| `pkg/speechkit/stt/vps` | Pure Go. | A reachable OpenAI-compatible whisper-server endpoint. | `Health` fails; the router treats the provider as unavailable. |
| `pkg/speechkit/stt/google`, `voiceagent/live/gemini` | Pure Go. | Network access plus the Google Cloud client/gRPC stack (large dependency graph). | Credential errors at `Transcribe`/session start. |
| Every other package | Pure Go. | None. | n/a |

Cloud providers (`stt/deepgram`, `stt/assemblyai`, `stt/openaicompat`,
`stt/openrouter`, `stt/huggingface`, the `voiceagent/live/*` providers) are
pure Go and need only network access and credentials; they never require a
native toolchain.

## Boundary Rules

1. Public SDK packages must not import `internal/*`. One documented adapter
   package is the exception: `pkg/speechkit/assist/skills` (adapts the public
   skill contract onto `internal/assist` and `internal/shortcuts`). It exists
   to expose an internal implementation through a stable public contract; it
   may not leak internal types into its exported API, and no further
   exceptions may be added without updating this document. The dependency for
   host configuration runs the other way: `pkg/speechkit/hostconfig` owns the
   loader semantics (`Defaults`, `Normalize`, `LoadConfig`, the hotkey /
   close-behavior / auth-mode normalisers) and `internal/config` delegates to
   it, so the desktop app and embedders read one `config.toml` identically
   while the SDK stays free of internal imports.
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
7. The root package `pkg/speechkit` holds contracts and value types only and
   imports no sibling package. Implementations live one level down:
   `pipeline` owns the capture-to-transcript engine, `catalog` owns the
   shipped provider/model data, and mode packages (`dictation`, `assist`,
   `tts`, `voiceagent`) compose them. A root import must never bring in
   provider data or the pipeline engine as a side effect.

## Naming And Contract Roles

The mode packages share one vocabulary so a host that has wired one mode can
wire the next without re-learning names:

| Concept | Name | Packages |
| --- | --- | --- |
| Mode entry point | `Service`, built by `NewService(Options) (*Service, error)` | `dictation`, `assist`, `tts`, `voiceagent` (`dictation.Runtime`/`NewRuntime` are the same type under the original name) |
| Constructor input | `Options` struct; required dependencies are validated in the constructor, never lazily | all mode packages, `companion`, `client` |
| Provider-facing contract (SPI) | `Provider` (`stt.STTProvider`, `tts.Provider`, `live.LiveProvider`, `voiceagent.Provider`) | provider packages implement these |
| Host-facing contract | Kernel interfaces in `pkg/speechkit` (`Transcriber`, `AudioRecorder`, `TranscriptOutput`, `Persistence`) | mode packages consume these |
| Bridge SPI → host contract | `stt.AsTranscriber`; `tts.Service` wraps `tts.Router` | never re-implemented by hosts |
| Fallback/selection | `Router` with a `Strategy` and per-instance hooks (`stt.Router.OnProviderSelected`) | `stt`, `tts` |
| Sentinel errors | Exported `Err*` variables wrapped with `%w`; branch with `errors.Is` | every public package; no message-text matching |
| Shared value types | One definition in `pkg/speechkit`, aliased where a subpackage needs its own name (`stt.WordConfidence = speechkit.WordConfidence`) | kernel owns shared shapes |

`speechkit.Transcriber` and `stt.STTProvider` are deliberately two contracts:
the first is what the runtime needs (WAV in, `Transcript` out, host language
policy applied), the second is what a backend offers (options, health, name,
routing metadata). Collapsing them would force every host to implement
`Health`/`Name` and every provider to know about host language policy.

## Provider Extension Points

A host adds a backend without touching the framework through three public
seams, documented end to end in [docs/sdk/custom-provider.md](../sdk/custom-provider.md):

| Seam | Surface | Guarantee |
| --- | --- | --- |
| SPI | `stt.STTProvider`, `tts.Provider`, `live.LiveProvider` | small interfaces over plain values; implementable outside the module |
| Conformance | `stt/sttcontract.RunContract`, `tts/ttscontract.RunContract`, `live/livecontract.Run` | the same invariants the shipped providers pass (identity, success stamping, error and cancellation propagation) |
| Catalog | `catalog.DefaultCatalog().With(profile...)` → `*catalog.Catalog` | validated (`catalog.ErrInvalidProfile`), unique (`catalog.ErrDuplicateProfileID`), immutable; provider id derived from the `<mode>.<provider>.<model>` id shape; feeds matrix, defaults and policy filtering |

The built-in slice functions (`catalog.DefaultProviderProfiles`,
`catalog.DefaultProviderMatrix`, `catalog.ProfilesForMode`, ...) stay as the
shipped-set view; `catalog.Catalog` is the composable one. The profile shape
itself (`speechkit.ProviderProfile`) stays in the root package so a host can
describe a provider without importing the shipped data. Hosts never need to
fork `catalog/catalog.go` to be listed.

## Current Verification

Verified on 2026-05-26 and updated on 2026-05-27 and 2026-06-02:

- `go test ./pkg/speechkit/...` passes with `CGO_ENABLED=0`.
- `go test ./pkg/speechkit/...` passes with MinGW cgo enabled.
- `go test ./examples/embed-companion ./examples/embed-tts ./examples/embed-event-bus` passes.
- Every entry-point package (`dictation`, `stt`, `assist`, `tts`, `client`,
  `hostconfig`) ships runnable `Example*` functions in `example_test.go` with
  `// Output:` blocks, so pkg.go.dev shows a verified first call and a drifted
  public signature fails `go test` instead of rotting in prose.
- Public export dry-run includes `pkg/speechkit/wakeword`, `pkg/speechkit/companion`, and `pkg/speechkit/tts`.
- Production SDK packages have no `internal/*` imports, except the single
  documented adapter package in Boundary Rule 1 (`pkg/speechkit/assist/skills`);
  `TestPublicSDKDoesNotImportInternalPackages` (`pkg/speechkit/sdk_boundary_test.go`)
  enforces the allowlist and fails on stale entries so it can only shrink.
- The root package imports no `pkg/speechkit/*` sibling; `pipeline` and
  `catalog` import the root, never the other way round. `go build` enforces
  this as an import cycle, so no separate test is needed.
- `internal/config` delegates its shared legacy backfills and normalisers to
  `pkg/speechkit/hostconfig`; `TestLoadAgreesWithPublicHostconfigLoaderOnLegacyFile`
  holds the desktop loader and `hostconfig.LoadConfig` to the same result on
  a legacy `config.toml`.
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
