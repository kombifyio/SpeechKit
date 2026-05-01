# SpeechKit

SpeechKit is a Windows-first voice system built around three products:

| Product | What it is | Start here |
| --- | --- | --- |
| Go Voice Framework | Embeddable Go APIs for Dictation, Assist, Voice Agent, provider routing, and mode contracts. | [Framework API](./docs/speechkit-framework-api.md) |
| Local Windows Client | Wails desktop app with global hotkeys, local-first dictation, settings, overlays, and optional cloud providers. | [Windows app](#local-windows-client) |
| SpeechKit Server | Containerized HTTP/WebSocket service for remote Dictation, Assist, and realtime Voice Agent workloads. | [Server docs](./docs/server/README.md) |

The shared rule is simple: Dictation only transcribes, Assist returns one-shot
utility or LLM output, and Voice Agent is realtime dialogue.

## Products

### Go Voice Framework

Use `pkg/speechkit` when you want SpeechKit inside another Go product.

```bash
go get github.com/kombifyio/SpeechKit/pkg/speechkit
```

Useful entry points:

- `speechkit.DefaultModeContracts()`
- `speechkit.DefaultProviderProfiles()`
- `speechkit.ProfilesForMode(mode)`
- `speechkit.ValidateProfileForMode(profile, mode)`

Examples live in [examples/](./examples/README.md).

### Local Windows Client

Download the latest Windows installer or portable bundle from
[GitHub Releases](https://github.com/kombifyio/SpeechKit/releases).

Default hotkeys:

- Dictation: `Win+Alt`
- Assist: `Ctrl+Win`
- Voice Agent: `Ctrl+Shift`

Local development:

```powershell
powershell -ExecutionPolicy Bypass -File .\start-dev.ps1
```

### SpeechKit Server

Use the SpeechKit Server when SpeechKit should run behind an HTTP/WebSocket API.

```bash
docker pull ghcr.io/kombifyio/speechkit-server:latest
```

Read [docs/server/README.md](./docs/server/README.md) for endpoints, auth,
deployment setup, and OpenAPI links.

## Documentation

This README is intentionally short. Jump into the detailed docs when needed:

- [Docs index](./docs/README.md)
- [Local OpenAPI](./docs/api/openapi.v1.yaml)
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
pkg/speechkit/          Go Voice Framework
cmd/speechkit/          Local Windows Client
cmd/speechkit-server/   SpeechKit Server entry point
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
