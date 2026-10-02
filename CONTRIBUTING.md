# Contributing to SpeechKit

SpeechKit is a beta Go framework and self-hostable speech server for
Dictation, Assist and Voice Agent workflows. This repository holds the Go
packages, the Linux server, CLI, MCP server, TypeScript clients, Android
modules, examples and documentation.

## Before you start

- Read [README.md](./README.md) for scope and module layout.
- Read [docs/README.md](./docs/README.md) for framework, server, MCP and API docs.
- Import only the public surface, `github.com/kombifyio/SpeechKit/pkg/speechkit/...`.
  Downstream applications must not import `app/internal/*`.

## Development setup

1. Install Go `1.26+`.
2. Install Node.js `24+` and pnpm for the TypeScript clients.
3. Install Docker (or any Docker-API engine) to run the server container locally.
4. Optional: install `gitleaks` for local secret scanning.

## Module layout

- The root module is the SDK (`pkg/speechkit/...`). The nested module `app/`
  holds the reference apps (`app/cmd`, `app/internal`). SDK code never imports
  the app module.
- The committed `go.work` joins both modules. Run app commands from the
  repository root with `./app/...` paths.
- Do not change the module path in `go.mod`; it is the import path every
  consumer depends on.

## Verification

Run these before opening a pull request:

```bash
go test ./pkg/... ./app/cmd/speechkit-cli/... ./app/cmd/speechkit-mcp/... ./examples/...
GOOS=linux CGO_ENABLED=0 go test ./app/cmd/speechkit-server/...
GOOS=linux CGO_ENABLED=0 go build ./app/cmd/speechkit-server ./app/cmd/speechkit-mcp ./app/cmd/speechkit-cli
go vet ./pkg/... ./app/cmd/speechkit-cli/... ./app/cmd/speechkit-mcp/... ./examples/...
node scripts/release/check-doc-links.mjs
gitleaks detect --source . --redact
```

TypeScript clients:

```bash
cd clients/typescript
pnpm install --frozen-lockfile
pnpm run build && pnpm run test && pnpm run typecheck
```

The examples run without provider credentials unless their README or source
says a live provider key is required:

```bash
go run ./examples/provider-catalog
go run ./examples/embed-companion
go run ./examples/embed-tts
go run ./examples/embed-event-bus
```

## Contribution rules

- Keep public API changes additive when possible. SpeechKit is pre-1.0, but
  breaking changes still need a clear changelog entry.
- Keep secrets in environment variables or local `.env` files. Never commit
  provider keys, bearer tokens, private hostnames or personal paths.
- Keep documentation links resolvable inside the repository.
- Keep examples small and runnable from the repository root.
- Public server deployments must use an authenticated `auth_mode`; never
  document `auth_mode = "none"` for a public bind.

## Changelog

User-facing changes belong in [CHANGELOG.md](./CHANGELOG.md). Write entries for
framework users and server operators.

## Pull requests

Include the surface you changed, the commands you ran, and any provider
credentials or live services you intentionally skipped. Small, focused pull
requests are easiest to review.
