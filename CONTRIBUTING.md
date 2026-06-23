# Contributing to SpeechKit

SpeechKit is a beta Go framework and self-hostable speech backend for
Dictation, Assist, and Voice Agent workflows. The public repository focuses on
the reusable Go packages, Linux server, CLI, MCP server, examples, docs, and
release artifacts. The Windows desktop client is distributed through GitHub
Releases, not developed directly in this public source tree.

## Before You Start

- Read [README.md](./README.md) for product scope and module boundaries.
- Read [docs/README.md](./docs/README.md) for framework, server, MCP, and API docs.
- Keep imports on the public surface: `github.com/kombifyio/SpeechKit/pkg/speechkit/...`.
- Do not import `internal/*` from downstream applications.

## Development Setup

1. Install Go `1.26+`.
2. Install Node.js `24+`.
3. Install Docker if you want to run the self-host server locally.
4. Optional: install `gitleaks` for local secret scanning.

## Repository Layout

| Path | Purpose |
|---|---|
| `pkg/speechkit/` | Public Go framework packages for embedders |
| `cmd/speechkit-server/` | Linux self-host server entry point |
| `cmd/speechkit-cli/` | CLI diagnostics and scaffolding |
| `cmd/speechkit-mcp/` | MCP server for agent docs, validation, and management |
| `internal/` | Implementation packages used by the public binaries |
| `examples/` | Runnable framework and server integration examples |
| `docs/` | Public framework, server, API, and agent documentation |
| `deploy/` | Dockerfile, Compose example, and server config templates |
| `release/latest/windows/` | Metadata mirror for the latest Windows release asset |

## Public Verification

Run these before opening a pull request:

```bash
go test ./pkg/... ./cmd/speechkit-cli/... ./cmd/speechkit-mcp/... ./examples/...
GOOS=linux CGO_ENABLED=0 go test ./cmd/speechkit-server/...
GOOS=linux CGO_ENABLED=0 go build ./cmd/speechkit-server ./cmd/speechkit-mcp ./cmd/speechkit-cli
go vet ./pkg/... ./cmd/speechkit-cli/... ./cmd/speechkit-mcp/... ./examples/...
node scripts/release/check-doc-links.mjs
gitleaks detect --source . --redact
```

The public examples should run without provider credentials unless their README
or source comments explicitly state that a live provider key is required:

```bash
go run ./examples/provider-catalog
go run ./examples/embed-companion
go run ./examples/embed-tts
go run ./examples/embed-event-bus
```

## Contribution Rules

- Keep public API changes additive when possible. SpeechKit is pre-1.0, but
  breaking changes still need a clear changelog entry.
- Keep secrets in environment variables or generated local `.env` files. Never
  commit provider keys, bearer tokens, private hostnames, or personal paths.
- Keep docs links resolvable in a fresh clone of the public repo.
- Keep examples small and runnable from the repository root.
- Keep server/browser auth guidance conservative: public deployments must not
  use `auth_mode = "none"`.
- Do not add private upstream files such as local planning docs, generated
  desktop build outputs, or internal-only workflow helpers to the public surface.

## Changelog

User-facing changes belong in [CHANGELOG.md](./CHANGELOG.md). Write entries for
framework users and server operators, not maintainers. The release lint checks
the `[Unreleased]` section and rendered release notes:

```bash
node scripts/release/lint-changelog.mjs --unreleased
node scripts/release/lint-version-sync.mjs
```

## Pull Requests

Include:

- the framework/server surface you changed
- the commands you ran
- any provider credentials or live services intentionally skipped
- screenshots only when changing visible docs or generated output

Small, focused pull requests are easiest to review.
