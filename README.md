# SpeechKit

> Local testing standard: use [docs/LOCAL_TESTING.md](docs/LOCAL_TESTING.md) as the repo-local source of truth before remote CI or deploy. Run the documented mise preflight gates before dispatching remote workflows.

SpeechKit is a Windows-first voice framework for products that need dictation,
voice commands, and realtime voice dialogue without coupling every use case to
one desktop app or one hosted API.

The framework currently has three modules:

| Module | What it is | Use it when |
| --- | --- | --- |
| Local-first Go backend | Embeddable Go runtime in `pkg/speechkit` with mode contracts, provider profiles, routing policy, readiness metadata, and reusable Dictation, Assist, and Voice Agent services. | You want to integrate SpeechKit into your own Go product, internal tool, prototype, or automation host. |
| SpeechKit Server | Linux server runtime in `cmd/speechkit-server` that wraps the same backend behind HTTP and WebSocket APIs. | You need a durable server process for remote clients, teams, product backends, browsers, or centrally managed model/provider configuration. |
| Windows Client | Wails desktop client in `cmd/speechkit` for local use, provider testing, and server-connected workflows. | You want to use SpeechKit on a Windows machine, validate providers and models, or connect a workstation to a SpeechKit Server. |

All three modules share the same three strict modes:

| Mode | Purpose | Boundary |
| --- | --- | --- |
| Dictation | Turn speech into text. | STT only. No LLM rewriting, no utilities, no codewords. |
| Assist | Turn speech or text into one useful result. | Codeword, utility, or LLM output with optional TTS and explicit UI surface metadata. |
| Voice Agent | Run realtime audio-to-audio dialogue. | Live conversation for brainstorming, support, and fast follow-ups. |

## Why SpeechKit

### Local-first Go backend

Use the backend when you want voice features inside another application without
adopting the Windows client. The public `pkg/speechkit` boundary gives host
apps stable mode contracts, service interfaces, provider catalogs, and
readiness data they can turn into their own setup UI.

Key advantages:

- One framework kernel for Dictation, Assist, and Voice Agent instead of three
  unrelated voice pipelines.
- Local-first provider support with room for managed local runtimes,
  user-managed local services, cloud providers, and direct vendor APIs.
- Host policy controls for enabled modes, fixed profiles, fallbacks, and clean
  vs intelligence behavior.
- Machine-readable readiness checks for credentials, local runtimes, model
  artifacts, and mode capability.

Start with [Framework API](./docs/speechkit-framework-api.md) or the examples in
[examples/](./examples/README.md).

### SpeechKit Server

Use the server when SpeechKit should run as a long-lived Linux service. It
adapts the same framework kernel to a containerized API surface so other
clients can call Dictation, Assist, and Voice Agent without embedding Go code.

Key advantages:

- One server image, one URL, and one deployment contract for all three modes.
- HTTP endpoints for Dictation and Assist plus WebSocket sessions for realtime
  Voice Agent.
- Built-in health/readiness routes, bearer or edge-auth modes, CORS/origin
  controls, and OpenAPI contracts.
- Centralized provider, model, and secret configuration for teams or hosted
  deployments.

Start with [docs/server/README.md](./docs/server/README.md) and the server
OpenAPI file at [docs/server/openapi.v1.yaml](./docs/server/openapi.v1.yaml).

### Windows Client

Use the Windows client when you want a ready-to-run desktop experience or a
reference host for testing providers, models, and server connections. The app
can run local-first on the machine or delegate selected work to a SpeechKit
Server.

Key advantages:

- Global hotkeys for Dictation, Assist, and Voice Agent.
- Local audio capture, VAD, overlays, settings, provider setup, and optional
  audio playback in one Wails app.
- Provider/model test bench for local, cloud, and direct integrations.
- Server connection support with configurable bearer-token environment variable,
  request timeout, and local fallback behavior.

Download public builds from
[GitHub Releases](https://github.com/kombifyio/SpeechKit/releases) when
available. For local development:

```powershell
powershell -ExecutionPolicy Bypass -File .\start-dev.ps1
```

Default hotkeys:

- Dictation: `Ctrl+Win`
- Assist: `Win+Alt`
- Voice Agent: `Ctrl+Shift`

## Quick Start

Embed the Go backend:

```bash
go get github.com/kombifyio/SpeechKit/pkg/speechkit
```

Run the Windows client locally:

```powershell
powershell -ExecutionPolicy Bypass -File .\start-dev.ps1
```

Run the server image:

```bash
docker pull ghcr.io/kombifyio/speechkit-server:latest
```

## Documentation

This README is the short orientation page. Use the detailed docs when you need
contracts, deployment steps, or release rules:

- [Docs index](./docs/README.md)
- [Framework API](./docs/speechkit-framework-api.md)
- [Local OpenAPI](./docs/api/openapi.v1.yaml)
- [SpeechKit Server docs](./docs/server/README.md)
- [SpeechKit Server OpenAPI](./docs/server/openapi.v1.yaml)
- [Deployment standards](./docs/deployment-standards.md)
- [OSS release boundary](./docs/oss-release-boundary.md)
- [Changelog](./CHANGELOG.md)

## Build

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

## Repository Layout

```text
pkg/speechkit/          Local-first Go backend
cmd/speechkit-server/   SpeechKit Server entry point
cmd/speechkit/          Windows Client entry point
frontend/app/           Windows UI source
Website/                Public website
internal/               Product internals
docs/                   Detailed documentation
deploy/                 Docker, Render, and server config
installer/              Windows installer
scripts/                Build, release, export, and verification scripts
```

## Trust

Public releases include checksums, an SBOM, and an unsigned Windows notice while
the no-cost unsigned release path is active. Download only from the official
[kombifyio/SpeechKit releases](https://github.com/kombifyio/SpeechKit/releases).

## License

Apache-2.0. See [LICENSE](./LICENSE).
