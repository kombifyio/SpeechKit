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

## Current Feature Set

- push-to-talk dictation with overlay feedback
- local runtime state and history via SQLite
- optional Hugging Face provider
- optional self-hosted VPS provider
- optional local whisper.cpp path
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

- Go `1.25+`
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
```

Public OSS users should rely on explicit configuration and environment variables. Internal development may additionally use Doppler, but public artifacts must never depend on private Doppler defaults.

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
cmd/voicetool/          Wails desktop host
frontend/app/           React/Vite UI sources
internal/audio/         Audio capture
internal/config/        Runtime config and explicit secret resolution
internal/router/        Provider routing
internal/secrets/       Host-side secret storage
internal/store/         Local storage
internal/stt/           Provider implementations
internal/frontendassets/ Generated embedded frontend assets
pkg/speechkit/          Framework-level orchestration
installer/              NSIS installer
scripts/                Canonical build and release scripts
docs/                   Architecture, release, and contributor docs
```

## OSS Release Hygiene

SpeechKit is prepared in a private upstream and mirrored into a separate release repository. Public publication is allowlist-based.

Start with:

- [`docs/deployment-standards.md`](./docs/deployment-standards.md)
- [`docs/oss-release-boundary.md`](./docs/oss-release-boundary.md)
- [`docs/oss-release-checklist.md`](./docs/oss-release-checklist.md)

## Contributing

See:

- [`CONTRIBUTING.md`](./CONTRIBUTING.md)
- [`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md)
- [`SECURITY.md`](./SECURITY.md)
- [`SUPPORT.md`](./SUPPORT.md)
- [`CHANGELOG.md`](./CHANGELOG.md)

## License

Apache-2.0. See [`LICENSE`](./LICENSE).
