# SpeechKit

SpeechKit is a Windows-first speech-to-text framework with a desktop host application. It is designed to be embedded into tools that want local-first dictation, optional cloud providers, and a clean host-managed credential model.

The repository treats `frontend/app` as first-class source. The embedded `internal/frontendassets/dist` output is generated from that source and should not be edited manually.

## What SpeechKit Is

- a Go framework for speech capture, routing, transcription, and desktop integration
- a Wails-based Windows desktop host that exercises the framework end to end
- a local-first runtime with optional provider integrations such as Hugging Face and self-hosted VPS endpoints

## Framework Principles

- provider-agnostic core
- tokenless framework layer
- host-managed credentials and secret storage
- local SQLite default for zero-config usage
- Windows-first release quality for the first public version
- three strict product modes: Dictation, Assist, and Voice Agent
- Gemini Live as the standard Voice Agent runtime with a durable framework prompt, an optional personal refinement prompt, and session policy

## Three Ways to Use SpeechKit

### As a Go Library

Use the framework backend in your own Go application without any UI:

```bash
go get github.com/kombifyio/SpeechKit/pkg/speechkit
```

Implement a handful of interfaces (`Transcriber`, `AudioRecorder`, `Persistence`) and the framework handles recording lifecycle, job queuing, and transcription routing. See [`examples/library/`](./examples/library/) for a working dictation pipeline and [`examples/provider-catalog/`](./examples/provider-catalog/) for the three-mode provider contract.

The public SDK owns and exposes the v23 framework catalog. Desktop and Windows-specific modules adapt this public catalog into their host runtime; they do not own the three-mode provider contract:

- `speechkit.DefaultModeContracts()` declares the strict Dictation, Assist, and Voice Agent contracts.
- `speechkit.DefaultProviderProfiles()` returns the reusable provider profile catalog for all three modes.
- `speechkit.ProfilesForMode(mode)` and `speechkit.ProviderKindsForMode(mode)` let host apps build their own settings UI without importing desktop internals.
- `speechkit.ValidateProfileForMode(profile, mode)` lets integrations reject profiles that would break a mode boundary.

The three mode contracts are stable at the framework boundary:

| Mode | Intelligence | Contract |
|------|--------------|----------|
| Dictation | User Intelligence | Audio in, text out. No LLM rewriting, no tool calling, no Assist utilities. |
| Assist | Utility Intelligence | One-shot utility or LLM result with optional TTS and result surface metadata. |
| Voice Agent | Brainstorming Intelligence | Realtime audio dialogue or explicit pipeline fallback with session summary support. |

### Through the Local Control API

The desktop host exposes a local HTTP control plane so external tools can configure and embed SpeechKit without linking Go code directly. Read-only introspection routes are available to local callers; mutating `PATCH` and `POST` routes require the control-plane token header or cookie.

| Endpoint | Purpose |
|----------|---------|
| `GET /api/v1/modes` | Returns mode contracts plus current per-mode settings. |
| `GET /api/v1/modes/{mode}/settings` | Returns one mode setting. `dictation`, `dictate`, `assist`, and `voice_agent` aliases are accepted. |
| `PATCH /api/v1/modes/{mode}/settings` | Updates enablement, hotkey behavior, active provider profile, TTS, dictionary, or Voice Agent summary settings. |
| `POST /api/v1/modes/{mode}/start` | Starts the selected mode through the framework command bus. |
| `POST /api/v1/modes/{mode}/stop` | Stops the selected mode through the framework command bus. |
| `GET /api/v1/providers/profiles` | Returns the provider catalog, active profiles, provider groups, and mode contracts. |
| `GET /api/v1/providers/readiness` | Reports credential, runtime, and capability readiness for every provider profile. |
| `GET /api/v1/providers/artifacts` | Returns Local Built-in and Local Provider artifacts plus current jobs. |
| `POST /api/v1/providers/artifacts/{artifactId}/download` | Downloads or pulls a provider artifact. |
| `POST /api/v1/providers/artifacts/{artifactId}/select` | Selects an already available provider artifact. |
| `POST /api/v1/providers/{profileId}/activate` | Activates a provider profile for its mode. |

This keeps SpeechKit reusable in existing software: a host can either embed the Go package or treat the Windows app as a local speech runtime with explicit mode and provider contracts. The Windows desktop app is one client implementation on top of the framework backend, not the source of truth for the framework catalog.

### As a Windows Desktop App

Download the installer from the [Releases](https://github.com/kombifyio/SpeechKit/releases) page:

- **SpeechKit-Setup.exe** â€” Windows installer
- **SpeechKit-Portable.zip** â€” portable bundle (no install required)

Windows artifacts may be unsigned while SpeechKit has no available no-cost public code-signing path. Public releases include `SHA256SUMS.txt`, `SpeechKit.sbom.json`, and `UNSIGNED-WINDOWS-RELEASE.txt` so users can verify the build origin and hashes before running the app.

## Current Feature Set

- push-to-talk Dictation with lightweight overlay feedback and no AI/tool routing
- local runtime state and history via SQLite
- six STT providers: local whisper.cpp, Hugging Face, OpenAI, Groq, Google, self-hosted VPS
- Assist mode for one-shot utilities, rewrites, summaries, and answer panels with optional TTS
- Voice Agent mode for realtime audio-to-audio dialogue (Gemini Live) with a dedicated live transcript surface and custom orb
- layered Voice Agent setup: host supplies API key, framework prompt, optional personal refinement prompt, and Gemini session policy
- Local Built-in Dictation model downloads via Whisper.cpp; Local Built-in Assist and Voice Agent use a bundled SpeechKit-managed llama.cpp server with downloadable GGUF model artifacts
- settings UI for provider, overlay, hotkey, and storage preferences

## Provider Credential Model

The framework core does not embed provider tokens.

For Hugging Face, the current host resolution order is:

1. user token stored from Settings
2. install token seeded by the installer and migrated on first start
3. environment variable fallback via `token_env`
4. explicit Doppler fallback for internal development only

That keeps the public framework neutral while allowing host apps to choose their own policy.

## Prerequisites

- Go `1.26+`
- Node.js `22+`
- MinGW-w64 for CGo on Windows
- NSIS for the canonical Windows build that emits the installer
- optional: ONNX Runtime DLL for Silero VAD
- optional: whisper.cpp server binary for local STT
- optional: Doppler CLI for internal development flows

## Quick Start

```bash
git clone https://github.com/kombifyio/SpeechKit.git
cd SpeechKit
powershell -ExecutionPolicy Bypass -File scripts/build.ps1
```

The canonical Windows build produces:

- `dist/windows/SpeechKit/SpeechKit.exe`
- `dist/windows/SpeechKit-Setup.exe`

## Runtime Configuration

The staged bundle includes `config.toml` next to `SpeechKit.exe`. For custom setups, start from `config.example.toml`.

```toml
[huggingface]
enabled = false
model = "openai/whisper-large-v3"
token_env = "HF_TOKEN"

[store]
backend = "sqlite"
save_audio = true
audio_retention_days = 7

[shortcuts.locale.de]
summarize = ["kurzfassung", "briefing"]
copy_last = ["kopier den letzten block"]
```

Public OSS users should rely on explicit configuration and environment variables. Internal development may additionally use Doppler, but public artifacts must never depend on private Doppler defaults.

Shortcut aliases are additive. SpeechKit keeps the built-in multilingual defaults and overlays any configured locale-specific aliases on top, so product teams can ship their own command words without changing Go code.

Default mode hotkeys are `Win+Alt` for Dictation, `Ctrl+Shift+J` for Assist, and `Ctrl+Shift+K` for Voice Agent.

## Voice Agent Live Test

For the first end-to-end Voice Agent run, keep the setup minimal:

1. Set `voice_agent_hotkey` in `config.toml` and keep `active_mode = "voice_agent"` only if you want Voice Agent preselected on startup.
2. Provide a Gemini API key through the env var referenced by `[providers.google].api_key_env` (default: `GOOGLE_AI_API_KEY`).
3. Keep `[voice_agent].framework_prompt = ""` if you want the built-in default helper, or supply your own durable framework prompt.
4. Optionally add `[voice_agent].refinement_prompt` for personal preferences that should sharpen the framework prompt without replacing it.
5. Use `model = "gemini-2.5-flash-native-audio-preview-12-2025"` for the current recommended default Voice Agent runtime.
6. Launch `SpeechKit.exe` and press the configured `voice_agent_hotkey` to start and stop the live session.

Notes:

- Native-audio Gemini Live sessions do not rely on `speechConfig.languageCode`; SpeechKit steers preferred language through the layered prompt assembly and locale-aware defaults.
- `enable_affective_dialog = true` automatically switches the Gemini Live client to `v1alpha` and is intended for Gemini 2.5 native-audio sessions, not Gemini 3.1 Flash Live.
- Non-blocking tool behavior is available in the Voice Agent framework contract, but Gemini 3.1 Flash Live only supports sequential tool execution.
- Voice Agent is a realtime-dialog surface. If the live runtime is unavailable, SpeechKit now keeps the mode boundary explicit instead of silently dropping into the Assist capture pipeline.

The Voice Agent now combines two prompt layers on every session:

1. `framework_prompt`: the durable host/framework instruction that defines the product behavior and fixed flows
2. `refinement_prompt`: the user-level personalization layer that sharpens tone, brevity, naming, or other preferences without replacing the framework layer

## Mode Boundaries

- **Dictation**: speech-to-text only, no codeword or utility routing
- **Assist**: one-shot utility mode that either inserts directly when safe or opens a reusable result panel
- **Voice Agent**: realtime spoken dialogue for brainstorming and quick clarification, not a work-product or insertion surface

## Build and Verification

```powershell
powershell -ExecutionPolicy Bypass -File scripts/build.ps1
```

This is the canonical verification path. It runs:

- frontend tests
- frontend lint
- frontend production build
- `go vet`
- `go test ./...`
- bundle build
- installer build

## Project Structure

```text
pkg/speechkit/          Framework-level orchestration (public API)
cmd/speechkit/          Wails desktop host application
frontend/app/           React/Vite UI sources
internal/audio/         Audio capture (WASAPI)
internal/stt/           STT provider implementations (6 providers)
internal/tts/           TTS provider implementations
internal/ai/            LLM integration via Genkit
internal/assist/        Assist mode pipeline (STT -> LLM -> TTS)
internal/voiceagent/    Voice agent (Gemini Live WebSocket)
internal/vad/           Voice activity detection (Silero ONNX)
internal/config/        Runtime config and secret resolution
internal/router/        Provider routing
internal/store/         Local storage (SQLite / PostgreSQL)
internal/secrets/       Host-side secret storage
internal/frontendassets/ Generated embedded frontend assets
examples/               Library usage examples
installer/              NSIS Windows installer
scripts/                Build and release scripts
docs/                   Architecture and contributor docs
```

## OSS Release Hygiene

SpeechKit is prepared in a private upstream and mirrored into a separate release repository. Public publication is allowlist-based.

Start with:

- [`docs/deployment-standards.md`](./docs/deployment-standards.md)
- [`docs/oss-release-boundary.md`](./docs/oss-release-boundary.md)
- [`docs/oss-release-checklist.md`](./docs/oss-release-checklist.md)
- [`docs/public-repo-operating-model.md`](./docs/public-repo-operating-model.md)

## Windows Artifact Trust

Public Windows releases are built from `kombifyio/SpeechKit`. If a trusted free signing provider is configured, the release workflow signs and verifies the Windows binaries. Until then, the supported no-cost path publishes unsigned Windows artifacts with checksums, SBOM, GitHub provenance when enabled, and an explicit unsigned-release notice.

See:

- [`docs/code-signing-policy.md`](./docs/code-signing-policy.md)
- [`docs/signpath-oss-setup.md`](./docs/signpath-oss-setup.md) for optional future SignPath setup

## Contributing

See:

- [`CONTRIBUTING.md`](./CONTRIBUTING.md)
- [`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md)
- [`SECURITY.md`](./SECURITY.md)
- [`SUPPORT.md`](./SUPPORT.md)
- [`CHANGELOG.md`](./CHANGELOG.md)

## License

Apache-2.0. See [`LICENSE`](./LICENSE).
