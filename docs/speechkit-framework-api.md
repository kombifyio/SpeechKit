# SpeechKit Framework API

SpeechKit v23 exposes the three product modes as a reusable framework boundary. Host applications can either embed `pkg/speechkit` directly or control the Windows desktop host through the local `/api/v1` control plane.

## Mode Contracts

| Mode | Intelligence | Input | Output | Boundary |
|------|--------------|-------|--------|----------|
| Dictation | User Intelligence | Audio | Text | STT only. No LLM rewriting, tool calling, codewords, or Assist utilities. |
| Assist | Utility Intelligence | Audio or text with optional context | One-shot result | Codeword, utility, LLM, optional TTS, and result surface metadata. |
| Voice Agent | Brainstorming Intelligence | Realtime audio dialogue | Dialogue transcript and optional summary | Native realtime audio first, explicit pipeline fallback when configured. |

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

Use `speechkit.RuntimePolicy` to constrain embedded deployments:

- `EnabledModes`: expose only one mode or a selected subset.
- `AllowedProfiles`: hide provider profiles that a host product does not support.
- `FixedProfiles`: force a concrete profile per mode.
- `AllowFallbacks`: allow or reject fallback profile selection.
- `ModeBehaviors`: declare Clean vs Intelligence behavior per mode.

The Windows desktop app remains the reference host and provider/model test bench. It can expose all profiles and switch between them, while embedded product integrations can use the same catalog with a narrower policy.

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
- `speechkit.ProfilesForMode(mode)`
- `speechkit.ProviderKindsForMode(mode)`
- `speechkit.ValidateDefaultCatalog()`

Provider profiles include stable IDs, provider group, execution mode, model variants, capabilities, and metadata that host applications can use to build their own settings UI.

`pkg/speechkit` is the framework boundary and the source of truth for the three strict mode profiles. The Windows desktop host adapts those public profiles into its internal runtime catalog and appends host-only support profiles such as TTS, utility, and embedding models. That keeps the backend reusable for other hosts while allowing the Windows module to remain a full reference client.

## Mode Settings

The API and SDK use the same per-mode settings shape.

| Mode | Settings |
|------|----------|
| Dictation | `enabled`, `hotkey`, `hotkeyBehavior`, `primaryProfileId`, `fallbackProfileId`, `dictionaryEnabled` |
| Assist | `enabled`, `hotkey`, `hotkeyBehavior`, `primaryProfileId`, `fallbackProfileId`, `ttsEnabled`, `utilityRegistry` |
| Voice Agent | `enabled`, `hotkey`, `hotkeyBehavior`, `primaryProfileId`, `fallbackProfileId`, `sessionSummary`, `pipelineFallback`, `closeBehavior` |

## Local Control API

The desktop host exposes a local HTTP API. Read-only introspection routes are available to local callers; mutating routes require the control-plane token through `ControlPlaneHeader` or `ControlPlaneCookie`.

The OpenAPI contract lives in [`docs/api/openapi.v1.yaml`](./api/openapi.v1.yaml).

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
| `/api/v1/dictionary` | `GET`, `POST` | Export dictionary entries with usage counters or import normalized dictionary entries. |
| `/api/v1/voice-sessions` | `GET` | List stored Voice Agent sessions and generated summaries. |

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
