# SpeechKit Framework API

SpeechKit v0.48 exposes the three product modes as a reusable framework boundary. Host applications can embed `pkg/speechkit` directly, run real providers in-process, call the self-host server, or control the Windows desktop host through the local `/api/v1` control plane. Long dictation and meeting capture build on the same boundary with segmented STT, provider-safe processing defaults, and system-audio meeting transcripts.

## Mode Contracts

| Mode | Intelligence | Input | Output | Boundary |
|------|--------------|-------|--------|----------|
| Dictation | User Intelligence | Audio | Text | STT only. No LLM rewriting, tool calling, codewords, or Assist utilities. |
| Assist | Utility Intelligence | Audio or text with optional context | One-shot result | Codeword, utility, LLM, optional TTS, and result surface metadata. |
| Voice Agent | Brainstorming Intelligence | Realtime audio dialogue | Dialogue transcript and optional summary | Native realtime audio first, explicit pipeline fallback when configured. |

Hands-Free is a capability layer over these modes, not a fourth mode. It
combines wake activation, microphone capture, auto-end policy, and optional
speaker output. Assist targets the Siri/Alexa-style Voice Companion path, Voice
Agent targets continuous dialogue, and Dictation remains UI-assisted because
text output needs a visible target or explicit commit surface.

Speaker diarization, speaker identification, and speaker attribution are also
capability layers over the same three modes, not new modes. Hosts opt in with
speaker options where a selected STT or streaming provider supports them.

Words and Replacements are the framework customization layer over the same
modes. A Word is recognition knowledge that biases STT and Voice Agent
perception. A Replacement is a deterministic transformation that can normalize
text, emit commands, expand snippets, or feed explicit templates. Native
Templates are versioned curated packs of the same data; `ActiveTemplateIDs`
selects which built-in or server-provided template data participates in
Dictation, Assist, and Voice Agent resolution. The dictionary-shaped API remains
a compatibility migration projection, not the extension model.

The public SDK exposes these contracts through:

- `speechkit.DefaultModeContracts()`
- `speechkit.ValidateProfileForMode(profile, mode)`
- `speechkit.RequiredCapabilities(mode, nativeRealtime)`

The mode-scoped service boundary is additive and does not replace the existing catalog/readiness API:

- `speechkit.DictationService` returns `DictationRun` values for strict STT-only runs.
- `speechkit.AssistService` returns `AssistResult` values with explicit `AssistSurfaceDecision` metadata.
- `speechkit.VoiceAgentService` starts, stops, and lists `VoiceAgentSession` records with `VoiceAgentSessionSummary`.

## Embeddable Mode Constructors

Host products can embed individual modes without importing the Windows desktop host:

- `pkg/speechkit/dictation.NewRuntime(...)` constructs a strict Dictation-only runtime from a host-provided `AudioRecorder`, `Transcriber`, optional output, optional store, and `RuntimePolicy`.
- `pkg/speechkit/assist.NewService(...)` constructs an Assist service from host-provided deterministic tools and/or an Assist generator. `ModeBehaviorClean` rejects unmatched LLM generation.
- `pkg/speechkit/voiceagent.NewService(...)` constructs a Voice Agent service from a host-provided realtime provider.

Host products can also import individual primitives without constructing a
full mode runtime:

| Use case | Public package |
| --- | --- |
| Wake activation only | `pkg/speechkit/wakeword` |
| Spoken output only | `pkg/speechkit/tts` |
| Hands-Free composition | `pkg/speechkit/companion` |
| Speaker diarization/attribution contracts | `pkg/speechkit/speaker` |
| Customization contracts | `pkg/speechkit/customize`, with runtime implementation in `internal/customize` and the semantic standard in `docs/words-and-replacements-standard.md` |
| Server-connected mode calls | `pkg/speechkit/client` |
| Embedded Voice Agent tools/session harness | `pkg/speechkit/agentkit`, `pkg/speechkit/voiceagent/live` |

Use `speechkit.RuntimePolicy` to constrain embedded deployments:

- `EnabledModes`: expose only one mode or a selected subset.
- `AllowedProfiles`: hide provider profiles that a host product does not support.
- `FixedProfiles`: force a concrete profile per mode.
- `AllowFallbacks`: allow or reject fallback profile selection.
- `ModeBehaviors`: declare Clean vs Intelligence behavior per mode.

The Windows desktop app remains the reference host and provider/model test bench. It can expose all profiles and switch between them, while embedded product integrations can use the same catalog with a narrower policy.

## Embeddable Hands-Free, Wake-Word, TTS, and Events

The current beta line keeps these public SDK packages additive against the existing mode contracts:

- `pkg/speechkit/wakeword` exposes wake-word phrase catalogs, detection events, dispatching, detector contracts, and `AutoEndPolicy`.
- `pkg/speechkit/wakeword/sherpa` adapts sherpa-onnx wake-word detection behind the public detector contracts. Builds without cgo still compile against the public no-cgo surface.
- `pkg/speechkit/tts` exposes `Provider`, `ProviderKind`, `Router`, `Service`, `NewService`, synthesis options, and fallback strategy for SDK hosts that need spoken output without importing desktop internals.
- `pkg/speechkit/companion.NewHandsFree(...)` composes wake detections, target-mode routing, host-provided transcript requests, Assist, Voice Agent activation, optional TTS, and Event-Bus publication for hands-free hosts. Set `Options.TargetMode` to `companion.TargetAssist`, `companion.TargetVoiceAgent`, or `companion.TargetDictationUIAssisted`.
- `pkg/speechkit/assist.Service` supports multi-turn `SessionKey`, skill context storage, codeword routing, TTS routing, and the optional `pkg/speechkit/assist/genkitadapter`.
- `pkg/speechkit/speaker` exposes speaker options, normalized words/segments, diarization results, provider profiles, streaming audio formats, and `SpeakerFrame` for realtime attribution. Provider-specific adapters stay in internal STT/router packages.
- `speechkit.Runtime.Events()` publishes additive Event-Bus events for wake detections, skill execution, companion sessions, Voice-Agent finalized turns, and TTS lifecycle.

Compatibility note: additive event metadata, Assist audio, and Assist follow-up
state use pointer wrapper types (`speechkit.Metadata`, `speechkit.AudioData`,
`assist.FollowupState`) so existing comparable SDK structs remain comparable.

The reference examples compile as Local-Library smoke tests:

| Example | Purpose |
|---------|---------|
| `examples/embed-companion` | Compose a hands-free Voice-Companion with mock wake/assist/TTS dependencies. |
| `examples/embed-tts` | Use the TTS service/router surface with a mock provider. |
| `examples/embed-event-bus` | Subscribe to and publish public runtime events. |

The CLI also ships Go-only scaffolds for agent-created companions:
`go-assist-voice-companion`, `go-voice-agent-companion`, and
`go-dictation-handsfree-ui`.

Release status: these SDK packages are part of the current public framework surface. Public API checks keep deprecated public fields compatible within the beta line unless a changelog entry explicitly calls out a breaking change.

Speaker Layer status: the provider-neutral public contracts are implemented in
the current beta surface. See
[`docs/capabilities/voice-capability-matrix.json`](./capabilities/voice-capability-matrix.json)
for provider support and auth status.

## Provider Catalog

Every main mode exposes the same four provider groups:

| Provider group | Purpose |
|----------------|---------|
| Local Built-in | SpeechKit-managed local runtime and model artifact path. |
| Local Provider | User-managed local runtime such as Ollama or another local OpenAI-compatible service. |
| Cloud Provider | Routed cloud or hosted open-weight provider. |
| Direct Provider | Direct model-vendor API. |

The SDK owns the reusable catalog through:

- `speechkit.DefaultProviderProfiles()`
- `speechkit.DefaultProviderMatrix()`
- `speechkit.DefaultProviderDefaults()`
- `speechkit.ProfilesForMode(mode)`
- `speechkit.ProviderKindsForMode(mode)`
- `speechkit.FindProviderDefault(provider, mode)`
- `speechkit.FindProviderMatrixRow(provider)`
- `speechkit.ValidateDefaultCatalog()`

Provider profiles include stable IDs, provider group, execution mode, model variants, capabilities, and metadata that host applications can use to build their own settings UI.

`DefaultProviderMatrix()` is the provider-agnostic view for settings, onboarding,
and Workbench-style clients. It normalizes the 10+ supported provider IDs into a
single matrix and classifies each feature as `native`, `routed`, `cascaded`,
`planned`, or `unsupported` for Dictation, streaming dictation, long
transcription, diarization, speaker identification, Assist, realtime voice, and
TTS. `DefaultProviderDefaults()` then selects one canonical profile per
provider/mode, preserving the stricter mode contracts from the profile catalog.
Each `ProviderDefault` carries the same canonical provider id, `authRequirement`,
`credentialRequired`, `credentialTarget`, and `transport` metadata so hosts can
render setup and readiness flows without hard-coding provider-specific branches
for OpenAI, Google STT, Deepgram, AssemblyAI, Hugging Face, OpenRouter, Ollama,
or local runtimes.

`pkg/speechkit` is the framework boundary and the source of truth for the three strict mode profiles. The Windows desktop host adapts those public profiles into its internal runtime catalog and appends host-only support profiles such as TTS, utility, and embedding models. That keeps the backend reusable for other hosts while allowing the Windows module to remain a full reference client.

## Mode Settings

The API and SDK use the same per-mode settings shape.

| Mode | Settings |
|------|----------|
| Dictation | `enabled`, `hotkey`, `hotkeyBehavior`, `primaryProfileId`, `fallbackProfileId`, transitional `dictionaryEnabled` for old local settings |
| Assist | `enabled`, `hotkey`, `hotkeyBehavior`, `primaryProfileId`, `fallbackProfileId`, `ttsEnabled`, `utilityRegistry` |
| Voice Agent | `enabled`, `hotkey`, `hotkeyBehavior`, `primaryProfileId`, `fallbackProfileId`, `sessionSummary`, `pipelineFallback`, `closeBehavior` |

Words/Replacements settings are cross-mode customizations, not per-mode
settings. They resolve through context filters such as mode, language, persona,
tags, active template IDs, and scope, then each mode consumes the resolved set.
`dictionaryEnabled` stays only as a transitional local setting for old configs.

## Local Control API

The desktop host exposes a local HTTP API. Read-only introspection routes are available to local callers; mutating routes require the control-plane token through `ControlPlaneHeader` or `ControlPlaneCookie`.

The OpenAPI contract lives in [`docs/api/openapi.v1.yaml`](./api/openapi.v1.yaml).
System-audio diagnostics that are intentionally operator-only live outside the
versioned control-plane contract. `/api/audio/loopback-selftest` checks bounded
WASAPI loopback capture while the desktop app is running. For release evidence,
use `scripts/smoke-system-loopback.ps1`; it plays a short synthetic tone through
the selected Windows output device, captures it via loopback, and writes a JSON
report under `.cache/loopback-smoke/` by default.

Long-dictation release evidence has a separate local gate:
`scripts/long-dictation-golden-gate.ps1` runs a five-minute fast-forwarded
golden fixture through the Dictation live segment queue, transcription worker,
duplicate ledger, paragraph handling, and final output. It writes
`.cache/long-dictation-gate/golden-long-dictation.json` by default. This proves
the kernel long-session behavior without cloud credentials; provider-backed
spoken audio evidence belongs in `scripts/long-dictation-provider-gate.ps1`,
which wraps `sk-e2e --strict-ready --require-functional` and requires
`--min-dictation-duration-ms` so short fixtures cannot pass the long-audio gate.

| Endpoint | Methods | Purpose |
|----------|---------|---------|
| `/api/v1/modes` | `GET` | Return mode contracts and current settings. |
| `/api/v1/modes/{mode}/settings` | `GET`, `PATCH` | Read or update one mode's settings. |
| `/api/v1/modes/{mode}/start` | `POST` | Start Dictation, Assist, or Voice Agent through the command bus. |
| `/api/v1/modes/{mode}/stop` | `POST` | Stop the selected mode through the command bus. |
| `/api/v1/providers/profiles` | `GET` | Return provider profiles, active profiles, provider groups, and contracts. |
| `/api/v1/providers/readiness` | `GET` | Return versioned credential, runtime, model-artifact, and capability readiness for each provider profile. |
| `/api/v1/providers/artifacts` | `GET` | Return downloadable or pullable Local Built-in and Local Provider artifacts plus current jobs. |
| `/api/v1/providers/artifacts/jobs` | `GET` | Return current provider artifact download/pull jobs. |
| `/api/v1/providers/artifacts/{artifactId}/download` | `POST` | Download or pull a provider artifact. |
| `/api/v1/providers/artifacts/{artifactId}/select` | `POST` | Select an already available provider artifact. |
| `/api/v1/providers/{profileId}/activate` | `POST` | Activate a provider profile for its mode. |
| `/api/v1/customization/words` | `GET`, `POST` | Read or replace Words for the selected language, source, and scope. |
| `/api/v1/customization/replacements` | `GET`, `POST` | Read or replace Replacements for the selected language, source, and scope. |
| `/api/v1/customization/lexicons` | `GET`, `POST` | Read or replace Lexicon collections. |
| `/api/v1/customization/rulesets` | `GET`, `POST` | Read or replace Ruleset collections. |
| `/api/v1/customization/templates` | `GET`, `POST` | Read the native Template Catalog and update the active template list. |
| `/api/v1/customization/templates/{templateId}/pack` | `GET` | Export a native template as a portable Customization Pack. |
| `/api/v1/customization/pack` | `GET`, `POST` | Export or import a portable Customization Pack. |
| `/api/v1/dictionary` | `GET`, `POST` | Migration projection for old local dictionary-shaped data. New integrations should use customization routes. |
| `/api/v1/voice-sessions` | `GET` | List stored Voice Agent session summaries without transcript or turn payloads. |
| `/api/v1/voice-sessions/{id}` | `GET` | Read one Voice Agent session with transcript, turns, and full summary detail. |
| `/api/v1/recording-sessions` | `GET`, `POST` | List or create long-running Dictation and Meeting recording sessions. |
| `/api/v1/recording-sessions/{id}` | `GET`, `DELETE` | Read or delete one recording session with ordered segment detail. |
| `/api/v1/recording-sessions/{id}/start` | `POST` | Start Dictation capture and bind finalized commits to the recording session. |
| `/api/v1/recording-sessions/{id}/pause` | `POST` | Pause bound Dictation capture while keeping the recording session active. |
| `/api/v1/recording-sessions/{id}/resume` | `POST` | Resume bound Dictation capture for a paused recording session. |
| `/api/v1/recording-sessions/{id}/stop` | `POST` | Stop Dictation capture without finishing the recording-session record. |
| `/api/v1/recording-sessions/{id}/segments` | `POST` | Append or replace one draft or finalized transcript segment by segment index. |
| `/api/v1/recording-sessions/{id}/finish` | `POST` | Mark the session finished and persist a supplied or derived summary. |
| `/api/v1/recording-sessions/{id}/summary` | `GET`, `POST` | Read or attach a recording-session summary. |
| `/api/v1/recording-sessions/{id}/summary-job` | `POST` | Start a non-blocking provider-capable summary job over ordered final segments. |

Accepted mode aliases include `dictation`, `dictate`, `transcribe`, `assist`, `voice_agent`, `voiceAgent`, and `voice-agent`.

Assist results use a compact surface contract so hosts do not need to infer UI behavior from text length alone:

| Surface | Meaning |
|---------|---------|
| `insert` | Safe direct insert or replacement in editable context. |
| `panel` | Keep the result visible in the Assist panel. |
| `bubble` | Short acknowledgement or actionable error only. |
| `silent` | Utility completed without user-facing output. |

Voice Agent session history is intentionally read-only in the local v1 API for now. Mutating or sensitive session actions should continue to go through token-gated control-plane routes when they are added.

## Readiness Model

`speechkit.Readiness` separates setup state into explicit fields:

- `configured`: the profile is selected or has enough configuration to be addressed.
- `credentialsReady`: required credentials are present, or the profile does not need credentials.
- `runtimeReady`: the local runtime, local model, hosted runtime, or build capability is available.
- `capabilityReady`: the profile satisfies its mode contract.
- `ready`: all readiness checks pass.

Since v0.23.1 each readiness item also includes:

- `schemaVersion`: currently `provider-readiness.v1`.
- `active` and `default`: selection metadata for host UIs.
- `executionMode`, `modelId`, and `source`: provider metadata needed for routing and setup labels.
- `requirements`: machine-readable checks such as credentials, local runtime, model file, and capability contract.
- `actions`: setup operations such as `configure_credential`, `download_artifact`, `select_artifact`, or `install_runtime`.
- `artifacts`: concrete Local Built-in or Local Provider model artifacts tied to that profile.

This lets hosts build setup flows without guessing provider-specific requirements or embedding secrets into the framework. Local Built-in Assist and Voice Agent profiles expose GGUF artifacts through the same contract, so external tools can download/select the model before activation. SpeechKit bundles and supervises the llama.cpp OpenAI-compatible server; the GGUF artifact is the model file loaded by that managed runtime.

The artifact model is split into a static catalog plus a status resolver. Static artifact metadata can be read without probing local runtimes; readiness uses bounded runtime checks and skips Ollama network probing by default, while the interactive artifact endpoint can include availability probes for the desktop setup UI.

## Voice UI Kit (`@kombifyio/speechkit-voice-ui`)

SpeechKit ships the Voice Assistant UI as a framework component: the
`@kombifyio/speechkit-voice-ui` npm package (`speechkit.voice_ui.v1`). It is
framework-neutral custom elements plus design tokens — no React, Svelte, or
LiveKit dependency — and it is the single standard UI module every SpeechKit
surface renders: the Windows client overlay, the server web page, the Android
Compose port, and any embedder.

The canonical element is `<speechkit-voice-assistant>`:

- `size="orb|compact|expanded"` and `frame="overlay|keyboard|watch|phone|panel"`
  select the surface shape; one element serves every host.
- `variant="aura|waveform"` selects the visual motif (Aura Orb default; Glass
  Waveform preset). The variant swaps only the motif — states, transcript,
  interaction, and localization are identical.
- `mark-src` places a host-provided brand image in the orb centre; the
  published kit is brand-neutral and ships no asset. The semantic mark
  vocabulary (`rosette|k|none`) and its URL mapping live in the `./marks`
  subpath so host settings agree on the same ids.
- The visual language is machine-readable in `tokens.json` (`assistant` and
  `assistant-variants` blocks) — native ports implement those tokens, never a
  re-interpretation.

Turnkey integration against any SpeechKit server (self-hosted or hosted):

```ts
import "@kombifyio/speechkit-voice-ui/define";
import { createVoiceAgentUiController } from "@kombifyio/speechkit-voice-ui/voiceagent-adapter";

const controller = createVoiceAgentUiController({
  serverUrl: "https://speechkit.example.com",
  token: sessionToken,
  start: { provider: "deepgram" },
});
document.querySelector("speechkit-voice-assistant").controller = controller;
```

The adapter owns microphone capture, the ticket WebSocket, playback with
barge-in flushing, and level metering; the provider that answers is whatever
the server's Voice Agent settings select (Deepgram and AssemblyAI are the
first-class managed providers). The kit itself holds no session FSM, provider
keys, tickets, or entitlement authority.

Normative details: `clients/typescript/packages/voice-ui/spec/voice-ui.spec.md`
and the package README.
