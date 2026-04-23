# Changelog

All notable changes to SpeechKit should be documented in this file.

The format is based on Keep a Changelog and this project is intended to ship under Apache-2.0.

## [Unreleased]

## [0.24.0] - 2026-04-23

### Added

- Embeddable Dictation-only runtime under `pkg/speechkit/dictation` for host products that only need strict STT.
- Public Assist and Voice Agent service constructors under `pkg/speechkit/assist` and `pkg/speechkit/voiceagent`.
- `RuntimePolicy` for constraining enabled modes, allowed provider profiles, fixed profiles, fallback behavior, and Clean vs Intelligence mode behavior.
- Public framework modularity implementation plan in `docs/plans/2026-04-23-framework-modularity-plan.md`.

### Changed

- `pkg/speechkit` no longer exposes `internal/*` types through the public Dictation recording and segmenting contracts.
- The library example now uses the new Dictation runtime builder with a fixed provider profile policy.
- Framework API docs now document the embeddable mode constructors and policy-based provider/model constraints.

## [0.23.2] - 2026-04-23

### Added

- Mode-scoped public service contracts for Dictation, Assist, and Voice Agent in `pkg/speechkit`.
- Assist utility registry with explicit utility IDs, input requirements, capability gates, and surface defaults.
- Voice Agent session persistence with stored turns, transcript, summaries, and `/api/v1/voice-sessions`.
- Dictionary import/export API with usage counts at `/api/v1/dictionary`.

### Changed

- Voice Agent GoAway reconnects now surface a visible `recovering` state in the session FSM and prompter UI.
- Assist tool routing now uses the registry contract for exact codeword utilities before LLM routing.
- Assist and Voice Agent overlay feedback controls now live in their dedicated mode settings instead of the general appearance settings.

### Fixed

- Assist no longer routes direct replies to the SpeechKit-managed local LLM endpoint when no downloaded GGUF model is selected and present.
- Local LLM connection failures now surface actionable Assist/Voice Agent guidance instead of raw loopback `connectex` errors.
- Restored the missing Beads parent issues `SK-002`, `SK-003`, and `SK-004` so the local backlog can import again.

## [0.23.1] - 2026-04-21

### Added

- **Versioned provider artifacts API**: `/api/v1/providers/artifacts`, `/api/v1/providers/artifacts/jobs`, and `/api/v1/providers/artifacts/{artifactId}/download|select` now expose Local Built-in model downloads and provider pulls through the same control plane as provider activation.
- **Structured provider readiness**: `/api/v1/providers/readiness` now includes `schemaVersion`, active/default flags, execution metadata, structured requirements, setup actions, and local model artifacts for external integrations.
- **Local Built-in model artifacts**: Dictation exposes Whisper.cpp downloads, while Assist and Voice Agent expose GGUF model artifacts for SpeechKit's bundled OpenAI-compatible llama.cpp runtime.

### Changed

- **Framework-owned provider catalog**: `pkg/speechkit` now owns the reusable three-mode provider catalog, while the Windows runtime adapts it and appends host-only support profiles.
- **Modular API v1 control plane**: mode settings, provider readiness, and artifact actions are split out of route registration into focused backend modules.
- **Separated artifact status resolution**: downloadable artifact metadata is now static by default, with file, runtime, and Ollama availability resolved through an explicit status layer.
- **Local Built-in Voice Agent selection**: pipeline fallback can now use a concrete downloaded local GGUF model while still reporting the stable `realtime.builtin.pipeline` profile.

### Fixed

- **Local Built-in profile selection**: selecting downloaded Dictation, Assist, or Voice Agent artifacts now persists the matching per-mode `ModelSelection`, so Voice Agent no longer remains active on the Gemini default after choosing the local pipeline.

### Security

- **Resolved HTTP target validation**: provider, download, and update requests now validate the resolved network target before dialing, closing SSRF paths that could bypass URL-only validation.
- **Plaintext secret fallback disabled**: non-Windows builds no longer fall back to a plaintext file secret store; unsupported secret-store access now fails closed.
- **Control-plane and provider error hardening**: frontend API calls attach the local control-plane token consistently, and provider-facing errors are redacted before surfacing to UI or logs.

### Removed

- **Frozen local artifacts**: removed the obsolete `frontend/app-v2` scaffold, tracked coverage/audio/install artifacts, and old static HTML prototypes from the release surface.

## [0.23.0] - 2026-04-21

### Highlights

- **API-first mode framework**: Dictation, Assist, and Voice Agent now expose reusable v23 mode contracts, provider profiles, provider groups, readiness models, and per-mode settings for SDK and API consumers.
- **Local control plane**: External tools can configure providers, inspect readiness, patch mode settings, and start or stop the three modes through the versioned `/api/v1` API.
- **Open-source framework boundary**: Public-safe framework API documentation and export coverage prepare the v23 surface for the `kombifyio/SpeechKit` release repository.
- **Versioned API contract**: The local `/api/v1` control plane now ships with a public OpenAPI contract for external integrations.

### Added

- **V23 API-first framework boundary**: `pkg/speechkit` now exposes strict Dictation, Assist, and Voice Agent contracts, reusable provider profiles, provider groups, mode settings, readiness models, and profile validation for host applications.
- **Versioned local control API**: The desktop host now exposes `/api/v1/modes`, `/api/v1/modes/{mode}/settings`, `/api/v1/modes/{mode}/start|stop`, `/api/v1/providers/profiles`, `/api/v1/providers/readiness`, and `/api/v1/providers/{profileId}/activate` for external tool integration.
- **Mode command bus controls**: Runtime commands can now start and stop Dictation, Assist, and Voice Agent explicitly through `mode.start` and `mode.stop`.
- **Public framework API documentation**: `docs/speechkit-framework-api.md` documents the v23 SDK and local API boundary for the public OSS export.
- **OpenAPI contract**: `docs/api/openapi.v1.yaml` describes the `/api/v1` mode and provider control-plane endpoints.

### Changed

- **Provider catalog contract tests**: The public framework catalog now verifies that every main mode exposes the four V23 provider groups and that Dictation profiles remain text-only without LLM or tool-calling capabilities.
- **OSS export boundary**: The public export allowlist now includes the public v23 framework API documentation without exposing internal architecture notes.

## [0.22.4] - 2026-04-20

### Fixed

- **Compact overlay placement**: Small Feedback now defaults to the lower screen edge, sits closer to the edge, and keeps the pill, dot, and dot menu correctly centered.
- **Voice Agent Small Feedback**: Voice Agent state changes now drive the compact feedback overlay for listening, processing, speaking, and final summary states.
- **Compact panel clipping**: Assist and Voice Agent feedback panels now reserve native host space so the compact panel is fully visible instead of being cut off.

## [0.22.1] - 2026-04-20

### Highlights

- **Clearer mode separation**: Dictation, Assist, and Voice Agent now follow stricter runtime boundaries so each mode behaves more predictably.
- **Voice Agent reliability**: Voice Agent now has a compact live panel, speaker selection, bounded mic streaming, echo suppression, and more stable listening/processing/speaking transitions.
- **Assist result handling**: Assist now routes one-shot utilities, visible result panels, and unsupported-provider guidance through clearer result contracts.

### Added

- **Voice Agent speaker selection**: the live Voice Agent panel can now list and switch output devices, with the selected speaker persisted.
- **Local provider split**: Assist and utility LLM profiles now distinguish built-in local models from externally managed local providers such as Ollama.
- **Assist result metadata**: Assist now models result surface and result kind, making panel-vs-action routing explicit.

### Changed

- **Voice Agent panel UX**: the live panel is more compact, shows the latest user and agent turns, and keeps longer sessions more responsive.
- **Mode boundaries**: Dictation stays passthrough-only, Assist uses its own result surfaces, and Voice Agent no longer falls back into Assist/capture surfaces.
- **Local STT timeout headroom**: local whisper transcription now scales its processing timeout with captured audio duration.

### Fixed

- **Voice Agent streaming stability**: mic streaming, follow-up turns, echo handling, and turn completion are more robust.
- **Voice Agent panel behavior**: transcript folding, activity feedback, and close/stop flows are more stable.
- **Assist error guidance**: unsupported model/provider failures now show more actionable Assist panel feedback.
- **Ollama local-provider downloads**: downloaded Ollama models can now activate their matching Assist or utility profile.

## [0.21.1] - 2026-04-18

### Highlights

- **Slimmer Windows release**: The installer and portable bundle now ship the verified whisper.cpp runtime without prebundled model weights, cutting the Windows download size far below the old ~500 MB package while keeping local model downloads available inside the app

### Added

- **Layered Voice Agent prompts**: Voice Agent sessions now combine a durable `framework_prompt` with an optional user-level `refinement_prompt`, so product teams can define fixed behavior while individual users still sharpen tone and working style
- **Voice Agent prompt settings**: The desktop Settings UI now exposes both prompt layers directly on the Voice Agent tab, and the runtime persists them through the normal settings/config flow

### Changed

- **Prompt composition contract**: Gemini Live prompt assembly now treats the framework prompt as the primary instruction layer, appends the refinement prompt with explicit precedence guidance, and still merges vocabulary plus locale steering on top
- **Legacy compatibility**: `[voice_agent].instruction` now acts as a compatibility alias for `framework_prompt`, so older configs continue to load cleanly while new installs use the explicit layered fields

## [0.19.1] - 2026-04-15

### Fixed

- **Overlay clipping**: The pill panel host window now matches the actual tri-mode control width, so the hover bubble no longer clips on the right edge
- **Idle overlay centering**: Pill and dot idle states now anchor against the dedicated host window bounds instead of viewport-sized roots, eliminating the visible right-shift in compact mode
- **Dedicated overlay switching**: The Wails overlay runtime now uses shared host metrics and simplified show/hide routing so anchor, panel, radial menu, and legacy overlay windows stay consistent

### Tests

- **Overlay geometry regression coverage**: Added backend tests for dedicated host metrics, anchored positions, and active window switching plus frontend tests that pin the compact overlay roots to the actual host window size

## [0.19.0] - 2026-04-15

### Added

- **Tri-mode hotkeys**: Dictation, Assist, and Voice Agent now persist independent hotkeys, while `active_mode` also supports `none` as an explicit deactivated state
- **Per-monitor overlay memory**: The movable pill overlay now stores a free position per monitor and restores the saved position when the active display changes
- **Voice Agent HF fallback profile**: The Voice Agent model catalog now exposes a Hugging Face pipeline-fallback profile so HF-backed models can be selected from the Voice Agent tab as well

### Changed

- **Overlay controls**: Bubble hover and dot context menu now show one icon per configured mode, and clicking the active mode deactivates it back to `none`
- **Recording status badge**: The pill now shows the active mode icon on the right edge while recording, processing, or speaking
- **Settings contract**: `assist_hotkey` and `voice_agent_hotkey` are now the canonical settings fields; legacy `agent_hotkey` and `agent_mode` remain compatibility inputs only

### Fixed

- **Hugging Face token setup**: HF credential management is available again through the model-card settings flow, including Voice Agent fallback profiles
- **Settings hotkey UX**: The General settings page no longer exposes a second runtime mode selector, and the built-in defaults are aligned again to `Win+Alt`, `Ctrl+Shift+J`, and `Ctrl+Shift+K`

## [0.18.0] - 2026-04-14

### Highlights

- **Local onboarding**: First-run setup now lets users choose Whisper Small or Whisper Large v3 Turbo, continue while downloads run in the background, and jump straight into Transcribe token setup instead of getting stuck in the wizard.
- **Recommended local model**: Whisper Large v3 Turbo is now the recommended local Whisper.cpp model, while fresh local installs no longer depend on a prebundled Small model.
- **Release surface automation**: The website homepage now derives its latest version and release highlights directly from `CHANGELOG.md`, and website deploys also trigger when the changelog changes.

### Added

- **Model download onboarding**: First-run setup now exposes Small and Turbo local model choices, a persistent in-app download progress dock, and cloud-provider escape hatches for Hugging Face or OpenAI setup
- **Starter model selection**: Users can choose which local Whisper model SpeechKit should use after setup even before the download has completed
- **StreamPlayer**: New `audio.StreamPlayer` type with `streamPipe` (sync.Cond-based io.Reader) for continuous buffered audio output — replaces per-chunk `PlayPCM` goroutine spawning that caused choppy/broken playback
- **Prompter stop button**: Voice Agent prompter window now shows a stop button (visible when agent is active) that emits a `voiceagent:stop` Wails event to deactivate the session from the UI
- **Session error lifecycle**: `cleanupOnError()` method on `voiceagent.Session` handles idle timer, context cancellation, provider close, state transition to Inactive, and `OnSessionEnd` callback
- **OnSessionEnd callback**: New callback in `voiceagent.Callbacks` fires on unexpected session termination (receive errors, GoAway without reconnect) — distinct from manual `Stop()` which does not fire it
- **Nil message guard**: Receive loop now handles nil messages from the provider (prevents panic on closed channels)
- **Integration tests**: 13 new tests covering error cleanup, GoAway-without-reconnect, manual stop semantics, streamPipe I/O (write/read, blocking, close, draining, idempotent close), and controller toggle/mic wiring

### Fixed

- **Onboarding flow**: The local-model step stays usable on smaller windows via more compact model cards and a sticky action footer, so Continue and token-setup actions remain visible during downloads
- **Local model routing**: Switching between downloaded local Whisper models no longer falls through to Hugging Face or other cloud STT routes
- **Overlay centering**: The compact pill and dot overlay positioning is corrected on scaled Windows displays so the anchor no longer drifts off-screen
- **Mic ownership**: `audioCapturer` is now wired to the `desktopInputController` — voice agent actually receives mic audio frames instead of silently getting nothing
- **Audio playback**: Voice agent audio output uses StreamPlayer with continuous buffering instead of spawning a new `PlayPCM` goroutine per chunk (which called `Stop()` on each invocation, killing previous audio)
- **Barge-in handling**: `OnInterrupted` now drains and restarts the StreamPlayer instead of just calling `audioPlayer.Stop()`
- **Deactivation cleanup**: Toggling off the voice agent now clears the PCM handler (`SetPCMHandler(nil)`) before stopping the session, stops the StreamPlayer, and updates prompter state
- **Error state cleanup**: Receive errors and GoAway-without-reconnect now transition session to Inactive, fire OnSessionEnd, stop the stream player, and hide the prompter — previously they left the session in a stale state
- **Capture start**: Voice agent activation now calls `audioCapturer.Start()` to begin the capture session, not just setting the handler

## [0.17.0] - 2026-04-12

### Highlights

Complete UI overhaul of the desktop application — the Dashboard, Settings, Quick Note, and overlay surfaces have been redesigned with a Material Design 3 dark theme featuring a purple accent palette. Business logic has been extracted into reusable headless hooks, and a new public marketing site ships alongside the release.

### Added

- **Marketing site**: Cloudflare Pages site in `Website/` with release-aware download links, Getting Started guide, Architecture overview, and Integrations page
- **Headless hooks architecture**: Extracted all Dashboard, Settings, and Library business logic into reusable hooks (`useSettings`, `useDashboardStats`, `useLibrary`, `useSetupWizard`, `useToast`, `useLogs`, `useErrorPolling`) with full test coverage
- **Quick Note window**: Standalone floating editor with Save, Record (arms next hotkey for dictation), LLM Summary, and Email Draft actions — includes recording indicator, word counter, and draft auto-save
- **Pinned notes on Dashboard**: Dashboard now highlights up to 3 pinned notes in a dedicated card; unpinned notes sorted by recency
- **Overlay entry points**: Separate CSS-isolated entry points for each overlay surface (pill anchor, pill panel, dot anchor, dot radial, assist bubble, quick capture, quick note) with transparent backgrounds
- **Credential source visibility**: Settings now shows whether active credentials come from a user token, install token, or environment fallback
- **Provider credential UI**: Save/clear/test buttons for HuggingFace, OpenAI, Google, and Groq API keys directly in Settings
- **Stable download URLs**: Release artifacts use fixed filenames (`SpeechKit-Setup.exe`, `SpeechKit-Portable.zip`) without version suffixes, enabling permanent download links

### Changed

- **Dashboard redesign**: Clean KPI row (Total Recordings, Avg WPM, Total Words, Recorded Minutes), Latest Transcription card with provider badge, Pinned Notes card, conditional update banner, and Welcome/Quick Start empty state
- **Settings redesign**: Two-column General tab with organized sections (Mode, Hotkeys, Microphone, Overlay, Storage, Vocabulary), streamlined STT/Assist/Voice Agent tabs with inline model setup and credential management
- **Design system**: Material Design 3 dark theme with purple accent (#cabeff / #947dff), surface hierarchy (#131318 → #1f1f25 → #35343a), Segoe UI Variable / Geist Variable font stack, consistent 0.625rem radius, thin subtle scrollbars, and signature gradient buttons
- **Overlay style options**: Pill (default) or Circle (focus) styles, Default or Kombify design variants for pill mode, position selector (Top/Bottom/Left/Right), movable toggle with drag instructions
- **Hotkey options expanded**: Ctrl+Win and Ctrl+Shift+[D/J/K/Space] now available alongside Windows+Alt
- **Audio retention controls**: Configurable auto-deletion (1/7/30/90 days) and max storage limit (MB)
- **Vocabulary input**: Bias transcription with custom term corrections using `spoken => canonical` mappings
- **Asset filenames**: Windows installer and portable bundle no longer contain version suffixes — `SpeechKit-Setup.exe` and `SpeechKit-Portable.zip` are now stable across releases
- Rebuilt all embedded frontend assets shipped with the desktop binary
- Updated website copy to match the current local-first, provider-agnostic architecture

### Fixed

- Local whisper.cpp startup now verifies that the runtime binary and model file are actually present before retrying startup, surfacing broken installs earlier instead of looping on a bad state
- Whisper model downloads now verify SHA256 checksums before activation so corrupt downloads are rejected instead of silently persisting
- OSS publish workflow now strips all private-repo workflows from the export and preserves the public repo's own workflow files during sync
- Website `package-lock.json` regenerated for npm 11 compatibility (missing `@emnapi/core` and `@emnapi/runtime` peer dependencies)
- Vitest config separated from Vite config to prevent `tsc` build errors from test-only type imports

## [0.16.0] - 2026-04-11

### Fixed

- Local whisper.cpp server startup: `Transcribe()` now blocks and waits for the server to finish loading instead of returning "not ready" immediately — hotkey presses during the first ~60 seconds after launch no longer silently fail

### Changed

- Fresh local installs now default to whisper.cpp (local-only routing) with HuggingFace disabled — users get an offline, zero-config experience out of the box without requiring a cloud token
- Added regression tests covering the startup-wait behavior: all three paths (success, failed startup, context cancellation) are now verified in CI on Windows with the race detector

## [0.15.2] - 2026-04-11

### Changed

- Renamed the internal `ModalityAgent` modality to `ModalityAssist` across backend and frontend to match the three user-facing modes: Dictate, Assist, Voice Agent
- Replaced outdated catalog models: Qwen 2.5 7B/32B → Qwen 3.5 9B/27B, GPT-4o/GPT-4o mini → GPT-5.4/GPT-5.4 mini
- Removed "Utility" from user-visible model setup tabs — utility models remain internal but are no longer a selectable category in the UI
- Updated OpenAI provider defaults in config from gpt-4o-mini/gpt-4o to gpt-5.4-mini/gpt-5.4
- Frontend mode button and hotkey label renamed from "Agent" to "Assist"

## [0.15.1] - 2026-04-10

### Fixed

- Simplified model setup to a maximum of four visible options per mode, with direct inline API key entry or local download actions on each model card
- Removed stale Settings copy and dead Hugging Face credential helpers left behind by the provider UI redesign
- Improved Windows installer metadata so setup and uninstall surfaces present clearer product information during the interim unsigned release

### Changed

- Moved the public OSS release path to `kombifyio/SpeechKit`, with GitHub-hosted workflows and SignPath-ready Windows release wiring prepared for the next signed release
- Published the OSS code signing policy and public-repo operating model used for the upcoming SignPath Foundation onboarding

## [0.15.0] - 2026-04-10

### Added

- Production readiness audit and hardening for multi-user test rollout
- Malformed `config.toml` now falls back to sane defaults instead of crashing on startup
- UI log notifications when AI providers or TTS audio player fail to initialize (degradation visibility)
- Test coverage for malformed config recovery path

### Changed

- Centralized data/config path resolution via `internal/runtimepath/` in store packages (SQLite, Postgres)
- Audit phases 1–6: real model names in catalog, crypto/rand usage, error leakage guards, MaxBytesReader on HTTP handlers, AppVersion via ldflags, downloads extraction to `internal/downloads/`, saveSettings decomposition, randHex error handling, overlay_position validation, CI coverage alignment

## [0.14.9] - 2026-04-09

### Fixed

- Credential saves were silently failing — frontend was sending `secret` but the backend `saveProviderCredential` and `testProviderCredential` routes expect `credential`; corrected both URLSearchParams calls
- API Keys section in Settings → Providers was hidden when no keys were configured (filtered by `available: true`); changed to show all providers unconditionally
- TypeScript TS2538 error: `executionMode` is optional on `ModelProfile`; guarded before using it as a `PROVIDER_FOR_EXECUTION_MODE` index

### Changed

- Settings → Providers tab restructured: Models section now appears first, API Keys section below (was reversed)
- Each model profile row shows an inline amber cue ("API key required — configure below ↓") when the required provider key is missing
- Added **Test** button to each API key row to validate a key before saving

## [0.14.8] - 2026-04-09

### Added

- Generic provider credential UI — users can now save, clear, and test API keys per provider (HuggingFace, OpenAI, Google, Groq) directly in Settings → Providers

### Fixed

- Replaced GitHub App auth in the OSS publish workflow with a direct `OSS_PUBLISH_TOKEN` PAT to eliminate intermittent token issuance failures
- Removed the unused `providerCredentialProvider` helper that was blocking staticcheck
- Updated releaseguard test to reflect the new OSS publish auth mechanism

## [0.14.7] - 2026-04-09

### Fixed

- Restored the OSS publish auth fallback so cross-repo source mirroring and release creation keep working without mandatory GitHub App credentials
- Made OSS tag sync idempotent and exported the Windows runtime preparation scripts so public `kombifyio/SpeechKit` tags can build Windows release artifacts again

## [0.14.6] - 2026-04-09

### Fixed

- Switched the OSS mirror workflow to dedicated `OSS_PUBLISH_TOKEN` HTTPS auth via `GIT_ASKPASS`, avoiding the failing inline credential path on the CI runner
- Updated OpenTelemetry dependencies to `v1.40.0` so `govulncheck` no longer blocks CI on the current release line
- Removed the zero-duration timing assumption from the STT HTTP provider tests so Windows release builds no longer fail on fast local test servers

## [0.14.5] - 2026-04-09

### Fixed

- Switched the Windows build script to call `npm.cmd` directly so GitHub Actions no longer routes frontend steps through the broken PowerShell wrapper path
- Moved CI and release workflows to Go `1.25.9` and updated `github.com/go-git/go-git/v5` to `v5.17.1` to clear the current `govulncheck` failures
- Normalized the OSS publish token before git access and removed the stale duplicate release block that would have broken the release workflow after a successful build

## [0.14.4] - 2026-04-09

### Fixed

- Switched Windows build entry points to `pwsh` so CI and tag builds no longer fall back to Windows PowerShell 5.1
- Switched OSS mirroring to explicit git-over-HTTPS token auth instead of relying on the checkout action's failing cross-repo auth path
- Cleared the current CI blockers in Staticcheck and Android lint for the `main` release path

## [0.14.3] - 2026-04-09

### Fixed

- Replaced the strict-mode-unsafe PowerShell release build invocation so tagged Windows releases can build again
- Hardened the OSS publish flow to validate mirror token access before checkout and reuse the same token source across mirror checkout and release upload

### Changed

- Bumped release identifiers across desktop, Android, installer metadata, and frontend artifacts to 0.14.3

## [0.14.1] - 2026-04-03

### Fixed

- Made the release workflow build from the requested Git tag during manual dispatch so published installers match the tagged source
- Fixed the OSS publish workflow to use workspace-safe checkout paths, remove the legacy `.public-export-v8` gitlink blocker, and mirror installer assets into the public `kombifyio/SpeechKit` release

## [0.14.0] - 2026-03-31

### Added

- Self-contained Windows release packaging that bundles `whisper-server`, required runtime DLLs, and the `ggml-small.bin` local model for installer and portable distributions
- Changesets-based versioning workflow for future release PRs

### Changed

- Switched the canonical Windows install layout to `%LOCALAPPDATA%\\SpeechKit` so the installer, bundled local runtime, and default config paths resolve consistently
- Updated shipped defaults and first-run local install behavior to prefer the bundled local runtime with dynamic routing
- Bumped release identifiers across desktop, Android, installer metadata, and frontend artifacts to 0.14.0

### Fixed

- Hardened Android release readiness around assistant wiring, secure token storage, deep links, onboarding checks, and CI coverage
- Replaced placeholder quick-note summary and email actions in the Windows host with working backend handlers

## [0.1.3] - 2026-03-30

### Fixed

- Removed deprecated `oto` player Close call (staticcheck SA1019)
- Removed unused `hideAssistBubble` method (staticcheck U1000)

### Changed

- Bumped version identifiers across all platforms to 0.1.3

## [0.1.0] - 2026-03-30

First public release of SpeechKit as an open-source speech framework.

### Added

- **Framework core** (`pkg/speechkit/`): interface-driven orchestration for recording, transcription, and output delivery — usable as a standalone Go library
- **Three operating modes**: Dictation (STT only), Assist (STT + LLM + TTS), Voice Agent (real-time audio-to-audio)
- **Six STT providers**: local whisper.cpp, Hugging Face, OpenAI, Groq, Google Cloud Speech, self-hosted VPS
- **TTS providers**: OpenAI TTS, Google Cloud TTS, local Kokoro
- **LLM integration** via Firebase Genkit with multi-provider support (Gemini, OpenAI, Groq, Ollama, Hugging Face)
- **Voice Agent mode** with Gemini Live WebSocket for real-time audio conversations
- **Windows desktop host** (Wails v3) with push-to-talk dictation, overlay feedback, system tray, and global hotkeys
- **Audio capture** via WASAPI (malgo) with voice activity detection (Silero ONNX)
- **Settings UI** for provider, overlay, hotkey, and storage preferences
- **Local SQLite storage** for transcription history with optional PostgreSQL backend
- **Provider-agnostic credential model**: tokenless framework core, host-managed secret storage via Windows Credential Manager
- **Canonical Windows build** producing both portable bundle and NSIS installer
- **CI/CD pipeline** with GitHub Actions (frontend tests, Go analysis, Windows build, automated releases)
- **Library usage example** (`examples/library/`) demonstrating framework integration without UI
- **First-run onboarding wizard** with microphone selection, hotkey configuration, and quick start guide
- **Error toast notifications** surfacing provider errors as user-visible messages
- **Automatic update check** against GitHub Releases with in-app notification banner
- **Feedback links** in system tray menu and welcome tab (GitHub Issues, Discussions)
- **Privacy policy** covering audio processing, local storage, and cloud provider data flows
- **Android app** with custom keyboard (HeliBoard), voice assistant service, live dashboard stats, and library UI
- **Android release build** configuration with environment-based signing
- **OSS governance**: Apache-2.0 license, contribution guidelines, security policy, export boundary enforcement
