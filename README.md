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
- Gemini Live as the standard Voice Agent runtime with a durable framework prompt, an optional personal refinement prompt, and session policy

## Three Ways to Use SpeechKit

### As a Go Library

Use the framework in your own Go application without any UI:

```bash
go get github.com/kombifyio/SpeechKit/pkg/speechkit
```

Implement a handful of interfaces (`Transcriber`, `AudioRecorder`, `Persistence`) and the framework handles recording lifecycle, job queuing, and transcription routing. See [`examples/library/`](./examples/library/) for a working example.

### As a Windows Desktop App

Download the installer from the [Releases](https://github.com/kombifyio/SpeechKit/releases) page:

- **SpeechKit-Setup.exe** â€” Windows installer
- **SpeechKit-Portable.zip** â€” portable bundle (no install required)

### As an Android App

The `android/` directory contains a Kotlin-based Android implementation with a custom keyboard (HeliBoard integration) and voice assistant service. Android support is under active development.

## Current Feature Set

- push-to-talk dictation with overlay feedback
- local runtime state and history via SQLite
- six STT providers: local whisper.cpp, Hugging Face, OpenAI, Groq, Google, self-hosted VPS
- assist mode with LLM-powered smart commands and TTS response
- voice agent mode with real-time audio-to-audio (Gemini Live)
- layered Voice Agent setup: host supplies API key, framework prompt, optional personal refinement prompt, and Gemini session policy
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
- If a Voice Agent profile uses a non-live Hugging Face fallback model, SpeechKit keeps the `voice_agent` mode active but routes capture through the STT -> agent -> output pipeline instead of Gemini Live.

The Voice Agent now combines two prompt layers on every session:

1. `framework_prompt`: the durable host/framework instruction that defines the product behavior and fixed flows
2. `refinement_prompt`: the user-level personalization layer that sharpens tone, brevity, naming, or other preferences without replacing the framework layer

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
android/                Android app and keyboard integration
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

## Code Signing

Public Windows releases are expected to be built from `kombifyio/SpeechKit`, signed, and verified before publication.

See:

- [`docs/code-signing-policy.md`](./docs/code-signing-policy.md)
- [`docs/signpath-oss-setup.md`](./docs/signpath-oss-setup.md)

## Contributing

See:

- [`CONTRIBUTING.md`](./CONTRIBUTING.md)
- [`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md)
- [`SECURITY.md`](./SECURITY.md)
- [`SUPPORT.md`](./SUPPORT.md)
- [`CHANGELOG.md`](./CHANGELOG.md)

## License

Apache-2.0. See [`LICENSE`](./LICENSE).
