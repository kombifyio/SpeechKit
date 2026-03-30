# Contributing to SpeechKit

SpeechKit is a Windows-first speech-to-text framework and desktop host. The repo is developed in a private upstream first and mirrored into a separate release repository through an explicit OSS release boundary. Contributions should keep that split intact.

## Before You Start

- Read [`README.md`](./README.md) for product scope and build prerequisites.
- Read [`docs/deployment-standards.md`](./docs/deployment-standards.md) for the canonical build and release contract.
- Read [`docs/oss-release-boundary.md`](./docs/oss-release-boundary.md) before changing build scripts, secrets, repo metadata, or release automation.

## Development Setup

1. Install Go `1.25+`.
2. Install Node.js `22+`.
3. Install MinGW-w64 and make sure `C:\msys64\mingw64\bin` is available.
4. Install NSIS if you want the canonical Windows build to also emit the installer.
5. Optional: install Doppler CLI for internal development flows. Doppler must never be required for OSS users.

## Canonical Verification

Run the canonical Windows build:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/build.ps1
```

This path is the source of truth for:

- frontend tests
- frontend lint
- frontend production build
- `go vet`
- `go test ./...`
- `dist/windows/SpeechKit/SpeechKit.exe`
- `dist/windows/SpeechKit-Setup.exe`

For targeted work you can also use:

```powershell
go test ./...
go vet ./...
cd frontend/app
npm test
npm run lint
```

## Pull Request Expectations

- Keep changes scoped. Separate feature work, refactors, and repo hygiene where possible.
- Add or update tests for behavior changes.
- Update docs when user-visible behavior, release steps, or contributor expectations change.
- Do not commit secrets, personal config, captured audio, or local runtime artifacts.
- Do not reintroduce private repo references, private Doppler defaults, or internal-only docs into the public surface.

## Issues and Feature Requests

- Use the issue templates for bugs and feature requests.
- Include reproduction steps, expected behavior, and platform details.
- For framework-facing changes, call out API or contract changes explicitly.

## Release Boundary Rules

- The OSS mirror/export path is allowlist-based.
- Files such as `AGENTS.md`, `CLAUDE.md`, personal notes, internal planning scraps, and runtime binaries are not public artifacts.
- The framework core must stay provider-agnostic and tokenless. Host apps own credential injection and secret storage policy.

## Code Style

- Prefer small, testable units.
- Keep comments short and high-signal.
- Preserve the existing Wails v3, Go, and React patterns unless there is a clear reason to change them.
- Default to ASCII unless the file already uses non-ASCII for a real reason.

## Security

If you discover a vulnerability, do not open a public issue. Follow [`SECURITY.md`](./SECURITY.md).
