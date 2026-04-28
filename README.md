# SpeechKit

SpeechKit is a Windows-first speech framework with a desktop reference app,
an embeddable Go API, and a containerized Server-Target. It is built for
products that need strict speech modes, local-first defaults, optional cloud
providers, and host-managed credentials.

The repository treats `frontend/app` as first-class source. The embedded
`internal/frontendassets/dist` output is generated from that source and should
not be edited manually.

## What You Get

| Variant | Use it when | Ships |
| --- | --- | --- |
| Device-Target | You want the Windows reference app | Wails desktop host, local overlay, global hotkeys, Settings UI |
| Local-Target | You embed SpeechKit into a Go app | `pkg/speechkit`, examples, mode contracts, provider catalog |
| Server-Target | You expose SpeechKit over HTTP/WebSocket | `speechkit-server`, REST endpoints, Voice Agent WebSocket |
| Voice Server | You scale Voice Agent separately | `speechkit-voice`, Voice Agent WebSocket only |

All variants share the same framework kernel. The Windows app is a reference
client, not the source of truth for the framework contract.

## Core Features

- three strict product modes: Dictation, Assist, and Voice Agent
- local-first Dictation with whisper.cpp support and optional cloud STT
- six STT provider paths: whisper.cpp, Hugging Face, OpenAI, Groq, Google, and self-hosted VPS
- Assist utilities for rewrites, summaries, answers, drafts, optional TTS, and visible result panels
- Voice Agent realtime dialogue through Gemini Live or an explicit pipeline fallback
- layered Voice Agent prompts: host/framework prompt plus optional personal refinement prompt
- local SQLite state by default, with storage contracts prepared for server deployments
- host-managed credentials; the framework core does not embed provider tokens
- public control-plane and server OpenAPI contracts for integrations

## Mode Boundaries

| Mode | Intelligence | Contract |
| --- | --- | --- |
| Dictation | User Intelligence | Audio in, text out. No LLM rewriting, no tools, no Assist routing. |
| Assist | Utility Intelligence | One-shot utility or LLM result with optional TTS and result surface metadata. |
| Voice Agent | Brainstorming Intelligence | Realtime spoken dialogue or explicit pipeline fallback with session summary support. |

Default mode hotkeys in the Windows reference app are `Win+Alt` for
Dictation, `Ctrl+Shift+J` for Assist, and `Ctrl+Shift+K` for Voice Agent.

## Start Here

- [Framework API](./docs/speechkit-framework-api.md) - embeddable Go API, mode contracts, provider catalog, and local control API.
- [Server-Target guide](./docs/server/README.md) - `speechkit-server`, `speechkit-voice`, mode endpoints, auth, and split deployments.
- [Server deploy guide](./docs/server/DEPLOY.md) - Docker Compose, Render, and generic OCI deployment notes.
- [Local OpenAPI](./docs/api/openapi.v1.yaml) - desktop control-plane contract.
- [Server OpenAPI](./docs/server/openapi.v1.yaml) - HTTP and WebSocket contract for the Server-Target.
- [Examples](./examples/README.md) - library and provider-catalog examples.
- [Docs index](./docs/README.md) - architecture, release, trust, and runbook links.

## Quick Start

### Windows App

Download the latest Windows artifacts from
[GitHub Releases](https://github.com/kombifyio/SpeechKit/releases):

- `SpeechKit-Setup.exe` - installer
- `SpeechKit-Portable.zip` - portable bundle

Public Windows releases include `SHA256SUMS.txt`, `SpeechKit.sbom.json`, and
`UNSIGNED-WINDOWS-RELEASE.txt` when the no-cost unsigned release path is
active.

### Go Library

```bash
go get github.com/kombifyio/SpeechKit/pkg/speechkit
```

Use the framework backend in your own Go application by implementing the
small host interfaces for audio recording, transcription, persistence, and
output delivery. See [`examples/library/`](./examples/library/) for a minimal
dictation pipeline and [`examples/provider-catalog/`](./examples/provider-catalog/)
for the three-mode provider contract.

Key public API entry points:

- `speechkit.DefaultModeContracts()`
- `speechkit.DefaultProviderProfiles()`
- `speechkit.ProfilesForMode(mode)`
- `speechkit.ProviderKindsForMode(mode)`
- `speechkit.ValidateProfileForMode(profile, mode)`

### Server-Target

```bash
docker pull ghcr.io/kombifyio/speechkit-server:latest
docker pull ghcr.io/kombifyio/speechkit-voice:latest
```

Use `speechkit-server` for Dictation REST, Assist REST, and Voice Agent
WebSocket from one container. Use `speechkit-voice` when Voice Agent should run
on its own scaling tier. See [`docs/server/README.md`](./docs/server/README.md).

## Runtime Configuration

The staged Windows bundle includes `config.toml` next to `SpeechKit.exe`. For
custom setups, start from `config.example.toml`.

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

Public OSS users should rely on explicit configuration and environment
variables. Internal development may use private secret managers, but public
artifacts must never depend on private defaults.

## Provider Credentials

SpeechKit's framework core is tokenless. Hosts decide how credentials are
stored and injected.

The Windows reference host resolves Hugging Face credentials in this order:

1. user token stored from Settings
2. install token seeded by the installer and migrated on first start
3. environment variable fallback via `token_env`
4. internal development fallback only when explicitly configured

Server deployments read secret values only from environment variables whose
names are configured in TOML.

## Build And Verification

Prerequisites:

- Go `1.26+`
- Node.js `22+`
- MinGW-w64 for CGo on Windows
- NSIS for installer builds
- optional: ONNX Runtime DLL for Silero VAD
- optional: whisper.cpp server binary for local STT

Canonical Windows app build:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/build.ps1 -SkipInstaller
```

Common checks:

```powershell
go test ./...
go vet ./...
npm --prefix frontend/app run test
npm --prefix frontend/app run build
npm --prefix Website run test
npm --prefix Website run build
```

## Project Structure

```text
pkg/speechkit/          Public framework orchestration API
cmd/speechkit/          Wails desktop host application
cmd/speechkit-server/   Linux Server-Target entry point
cmd/speechkit-voice/    Linux Voice Server entry point
frontend/app/           React/Vite Windows UI sources
Website/                Svelte/Vite public website
internal/audio/         WASAPI capture and playback
internal/stt/           STT provider implementations
internal/tts/           TTS provider implementations
internal/ai/            LLM integration
internal/assist/        Assist mode pipeline
internal/voiceagent/    Voice Agent runtime
internal/server/        Server-Target HTTP/WebSocket adapters
internal/serverclient/  Device-to-server transport adapters
internal/store/         SQLite/Postgres storage contracts
deploy/                 Docker, Render, and server config
docs/                   Architecture, release, server, and runbook docs
examples/               Library usage examples
installer/              NSIS Windows installer
scripts/                Build, release, export, and verification scripts
```

## Release And Trust

SpeechKit is prepared in a private upstream and mirrored into
`kombifyio/SpeechKit` through an allowlisted public export.

Start with:

- [deployment standards](./docs/deployment-standards.md)
- [OSS release boundary](./docs/oss-release-boundary.md)
- [OSS release checklist](./docs/oss-release-checklist.md)
- [public repo operating model](./docs/public-repo-operating-model.md)
- [code signing policy](./docs/code-signing-policy.md)

## Contributing

See:

- [`CONTRIBUTING.md`](./CONTRIBUTING.md)
- [`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md)
- [`SECURITY.md`](./SECURITY.md)
- [`SUPPORT.md`](./SUPPORT.md)
- [`CHANGELOG.md`](./CHANGELOG.md)

## License

Apache-2.0. See [`LICENSE`](./LICENSE).
